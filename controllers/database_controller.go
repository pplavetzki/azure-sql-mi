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
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"

	"github.com/go-logr/logr"
	actionsv1alpha1 "github.com/pplavetzki/azure-sql-mi/api/v1alpha1"
	ms "github.com/pplavetzki/azure-sql-mi/internal"
)

const databaseFinalizer = "actions.msft.isd.coe.io/finalizer"

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type AnnotationPatch struct {
	Logger     *log.DelegatingLogger
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
	return r.Client.Status().Update(context.TODO(), db, &client.UpdateOptions{})
}

//+kubebuilder:rbac:groups=actions.msft.isd.coe.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=actions.msft.isd.coe.io,resources=databases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=actions.msft.isd.coe.io,resources=databases/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

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
	logger := log.Log

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
	sec := &corev1.Secret{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: db.Spec.Credentials.Name, Namespace: db.Namespace}, sec)
	if err != nil {
		logger.Error(err, "secrets credentials resource not found. failed to get CredentialsSecrets")
		return ctrl.Result{}, err
	}

	username := sec.Data[db.Spec.Credentials.UsernameKey]
	password := sec.Data[db.Spec.Credentials.PasswordKey]
	/******************************************************************************************************************/

	// This is the creating a MSSql Server `Provider`
	msSQL := ms.NewMSSql(db.Spec.Server, string(username), string(password), db.Spec.Port)

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
			if err = r.finalizeDatabase(ctx, logger, db, msSQL); err != nil {
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

	var dbName *string
	var currentDBName string

	setID := db.Annotations["mssql/db_id"]
	if setID != "" {
		valId, _ := strconv.Atoi(setID)
		dbName, _ = msSQL.FindDatabaseName(ctx, valId)

		if dbName != nil {
			currentDBName = *dbName
		}
		// Are we doing a Alter here???
		if currentDBName != "" && db.Spec.Name != currentDBName {
			logger.Info("current db name", "have", currentDBName, "want", db.Spec.Name)
			logger.Info("want to do an update of the database name if we can")
		}
	} else {
		var dbID string
		meta.SetStatusCondition(&db.Status.Conditions, *db.CreatingCondition())
		r.updateDatabaseStatus(db, "Creating")
		id, err := msSQL.CreateDatabase(ctx, db)
		if err != nil {
			r.updateDatabaseStatus(db, "Error")
			logger.Info("failed to create the database", "name", err.Error())
			return ctrl.Result{}, err
		}
		if id != nil {
			dbID = *id
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to return the database id for name: %s", db.Spec.Name)
		}
		pw := AnnotationPatch{
			Logger:     logger,
			DatabaseID: dbID,
		}
		err = r.Patch(ctx, db, pw)
		if err != nil {
			logger.Info("failed to patch the database with the database id", "error", err.Error())
			return ctrl.Result{}, err
		}
		meta.SetStatusCondition(&db.Status.Conditions, *db.CreatedCondition())
		r.updateDatabaseStatus(db, "Created")
	}

	return ctrl.Result{}, nil
}

func (r *DatabaseReconciler) finalizeDatabase(ctx context.Context, reqLogger logr.Logger, db *actionsv1alpha1.Database, mssql *ms.MSSql) error {
	if err := mssql.DeleteDatabase(ctx, db); err != nil {
		return err
	}
	reqLogger.Info("Successfully finalized database")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&actionsv1alpha1.Database{}).
		Complete(r)
}
