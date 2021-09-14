/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"
	actionsv1alpha1 "github.com/pplavetzki/azure-sql-mi/api/v1alpha1"
	ms "github.com/pplavetzki/azure-sql-mi/internal"
	batch "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const databaseFinalizer = "actions.msft.isd.coe.io/finalizer"

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

type AnnotationPatch struct {
	Logger     logr.Logger
	DatabaseID string
}

func (a AnnotationPatch) Type() types.PatchType {
	return types.MergePatchType
}

func (a AnnotationPatch) Data(obj client.Object) ([]byte, error) {
	annotations := obj.GetAnnotations()
	annotations["mssql/db_id"] = a.DatabaseID
	a.Logger.Info("value of annotations", "mssql/db_id", annotations["mssql/db_id"])

	obj.SetAnnotations(annotations)
	return json.Marshal(obj)
}

func (r *DatabaseReconciler) updateDatabaseStatus(db *actionsv1alpha1.Database, status string) error {
	db.Status.Status = status
	return r.Status().Update(context.TODO(), db)
}

//+kubebuilder:rbac:groups=actions.msft.isd.coe.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=actions.msft.isd.coe.io,resources=databases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=actions.msft.isd.coe.io,resources=databases/finalizers,verbs=update
//+kubebuilder:rbac:groups=sql.arcdata.microsoft.com,resources=sqlmanagedinstances,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Database object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	logger := r.Logger
	logger.Info("reconciling database")

	db := &actionsv1alpha1.Database{}
	err := r.Get(ctx, req.NamespacedName, db)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("Database resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "failed to get Database")
		return ctrl.Result{}, err
	}

	/*******************************************************************************************************************
	* Quering the defined secret for the database connection
	*******************************************************************************************************************/
	mi, err := ms.QuerySQLManagedInstance(ctx, db.Namespace, db.Spec.SQLManagedInstance)
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.V(1).Info("successfully found managed instance", "sql-managed-instance", db.Spec.SQLManagedInstance)
	if mi.Status.State != "Ready" {
		meta.SetStatusCondition(&db.Status.Conditions, *db.ErroredCondition())
		r.updateDatabaseStatus(db, "Error")
		return ctrl.Result{}, fmt.Errorf("the sql managed instance is not in a `Ready` state, current status is: %v", mi.Status)
	}
	sec := &corev1.Secret{}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: mi.Spec.LoginRef.Name, Namespace: mi.Spec.LoginRef.Namespace}, sec)
	if err != nil {
		logger.Error(err, "secrets credentials resource not found", "secret-name", mi.Spec.LoginRef.Name)
		return ctrl.Result{}, err
	}

	username := sec.Data["username"]
	password := sec.Data["password"]
	/******************************************************************************************************************/

	// This is the creating a MSSql Server `Provider`
	// db.Spec.Server
	msSQL := ms.NewMSSql("localhost", string(username), string(password), db.Spec.Port)

	// Let's look at the status here first

	/*******************************************************************************************************************
	* Finalizer to check what to do if we're deleting the resource
	*******************************************************************************************************************/
	if db.ObjectMeta.DeletionTimestamp.IsZero() {
		// Add finalizer for this CR
		if !controllerutil.ContainsFinalizer(db, databaseFinalizer) {
			controllerutil.AddFinalizer(db, databaseFinalizer)
			err = r.Update(ctx, db)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(db, databaseFinalizer) {
			if err = r.finalizeDatabase(ctx, db, msSQL); err != nil {
				return ctrl.Result{}, err
			}
		}
		controllerutil.RemoveFinalizer(db, databaseFinalizer)
		if err := r.Update(ctx, db); err != nil {
			return ctrl.Result{}, err
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}
	/******************************************************************************************************************/

	/*******************************************************************************************************************
	* Let's do sync logic here...
	/******************************************************************************************************************/
	isJobFinished := func(job *batch.Job) (bool, batch.JobConditionType) {
		for _, c := range job.Status.Conditions {
			if (c.Type == batch.JobComplete || c.Type == batch.JobFailed) && c.Status == corev1.ConditionTrue {
				return true, c.Type
			}
		}

		return false, ""
	}

	var childJobs batch.JobList
	var status string
	var condition metav1.Condition

	if err := r.List(ctx, &childJobs, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}); err != nil {
		logger.Error(err, "unable to list child Jobs")
		return ctrl.Result{}, err
	}
	if len(childJobs.Items) == 0 {
		job, err := r.createSyncJob(db, mi, msSQL)
		if err != nil {
			logger.Error(err, "unable to create sync job")
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, job); err != nil {
			logger.Error(err, "unable to create Job for Sync", "job", job)
			return ctrl.Result{}, err
		}
	} else {
		for _, job := range childJobs.Items {
			_, finishedType := isJobFinished(&job)
			switch finishedType {
			case "": // ongoing
				status = "Pending"
				condition = *db.CreatingCondition()
			case batch.JobFailed:
				status = "Errored"
				condition = *db.ErroredCondition()
			case batch.JobComplete:
				status = "Synced"
				condition = *db.CreatedCondition()
				loggies, err := ms.QueryJobPod(ctx, db.Namespace, job.Name)
				if err != nil {
					logger.Error(err, "failed to get logs from job", "job", job.Name)
				}
				logger.V(1).Info(*loggies)
			}
		}
	}

	meta.SetStatusCondition(&db.Status.Conditions, condition)
	r.updateDatabaseStatus(db, status)
	/******************************************************************************************************************/

	// var dbName *string

	// setID := db.Annotations["mssql/db_id"]
	// if setID != "" {
	// 	valId, _ := strconv.Atoi(setID)
	// 	dbName, err = msSQL.FindDatabaseName(ctx, valId)

	// 	// This means that the database has probably been deleted outside of the CRD
	// 	if err != nil {
	// 		return ctrl.Result{}, err
	// 	}
	// 	if dbName == nil {
	// 		return ctrl.Result{}, fmt.Errorf("database not found, has it been deleted?")
	// 	}

	// 	meta.SetStatusCondition(&db.Status.Conditions, *db.UpdatingCondition())

	// 	err := msSQL.AlterDatabase(ctx, db)
	// 	if err != nil {
	// 		meta.SetStatusCondition(&db.Status.Conditions, *db.ErroredCondition())
	// 		r.updateDatabaseStatus(db, "Error")
	// 		logger.Info("failed to alter the database", "name", err.Error())
	// 		return ctrl.Result{}, err
	// 	}

	// 	meta.SetStatusCondition(&db.Status.Conditions, *db.UpdatedCondition())
	// } else {
	// 	var dbID string
	// 	meta.SetStatusCondition(&db.Status.Conditions, *db.CreatingCondition())
	// 	id, err := msSQL.CreateDatabase(ctx, db)
	// 	if err != nil {
	// 		meta.SetStatusCondition(&db.Status.Conditions, *db.ErroredCondition())
	// 		r.updateDatabaseStatus(db, "Error")
	// 		logger.Info("failed to create the database", "name", err.Error())
	// 		return ctrl.Result{}, err
	// 	}
	// 	if id != nil {
	// 		dbID = *id
	// 	} else {
	// 		return ctrl.Result{}, fmt.Errorf("failed to return the database id for name: %s", db.Spec.Name)
	// 	}
	// 	pw := AnnotationPatch{
	// 		Logger:     logger,
	// 		DatabaseID: dbID,
	// 	}
	// 	err = r.Patch(ctx, db, pw)
	// 	if err != nil {
	// 		logger.Info("failed to patch the database with the database id", "error", err.Error())
	// 		return ctrl.Result{}, err
	// 	}
	// 	meta.SetStatusCondition(&db.Status.Conditions, *db.CreatedCondition())
	// }

	// r.updateDatabaseStatus(db, "Created")
	return ctrl.Result{}, nil
}

func (r *DatabaseReconciler) finalizeDatabase(ctx context.Context, db *actionsv1alpha1.Database, mssql *ms.MSSql) error {
	if err := mssql.DeleteDatabase(ctx, db.Spec.Name); err != nil {
		return err
	}
	return nil
}

var (
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = actionsv1alpha1.GroupVersion.String()
)

func (r *DatabaseReconciler) createSyncJob(db *actionsv1alpha1.Database, mi *ms.SQLManagedInstance, msSQL *ms.MSSql) (*batch.Job, error) {
	// We want job names for a given nominal start time to have a deterministic name to avoid the same job being created twice
	sched := time.Now()
	name := fmt.Sprintf("%s-%d", db.Name, sched.Unix())
	endpointInfo := strings.Split(mi.Status.PrimaryEndpoint, ",")

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   db.Namespace,
		},
		Spec: batch.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "sync",
							Image: "paulplavetzki/sync:v0.0.2",
							// EnvFrom: []corev1.EnvFromSource{
							// 	corev1.EnvFromSource{
							// 		SecretRef: &corev1.SecretEnvSource{
							// 			LocalObjectReference: corev1.LocalObjectReference{
							// 				Name: "jumpstart-sql-login-secret",
							// 			},
							// 		},
							// 	},
							// },
							Env: []corev1.EnvVar{
								{
									Name:  "DB_PASSWORD",
									Value: msSQL.Password,
								},
								{
									Name:  "DB_USER",
									Value: msSQL.User,
								},
								{
									Name:  "DB_NAME",
									Value: db.Spec.Name,
								},
								{
									Name:  "DB_PORT",
									Value: endpointInfo[1],
								},
								{
									Name:  "MS_SERVER",
									Value: endpointInfo[0],
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
	// for k, v := range cronJob.Spec.JobTemplate.Annotations {
	// 	job.Annotations[k] = v
	// }
	// job.Annotations[scheduledTimeAnnotation] = sched.Format(time.RFC3339)
	// for k, v := range cronJob.Spec.JobTemplate.Labels {
	// 	job.Labels[k] = v
	// }
	if err := ctrl.SetControllerReference(db, job, r.Scheme); err != nil {
		return nil, err
	}

	return job, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &batch.Job{}, jobOwnerKey, func(rawObj client.Object) []string {
		// grab the job object, extract the owner...
		job := rawObj.(*batch.Job)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}
		// ...make sure it's a CronJob...
		if owner.APIVersion != apiGVStr || owner.Kind != "Database" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&actionsv1alpha1.Database{}).
		Owns(&batch.Job{}).
		Complete(r)
}
