package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DatabaseConditionCreating string = "Creating"
	DatabaseConditionCreated  string = "Created"
)

const (
	DatabaseConditionReasonCreating string = "CreatingDatabase"
	DatabaseConditionReasonCreated  string = "CreatedDatabase"
)

func (d *Database) CreatingCondition() *metav1.Condition {
	return &metav1.Condition{Type: DatabaseConditionCreating, Status: metav1.ConditionTrue,
		Reason: DatabaseConditionReasonCreating, Message: "Database is being created"}
}

func (d *Database) CreatedCondition() *metav1.Condition {
	return &metav1.Condition{Type: DatabaseConditionCreated, Status: metav1.ConditionTrue,
		Reason: DatabaseConditionReasonCreated, Message: "Database successfully created"}
}
