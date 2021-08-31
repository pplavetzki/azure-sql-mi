package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DatabaseConditionCreating string = "Creating"
	DatabaseConditionCreated  string = "Created"
	DatabaseConditionError    string = "Errored"
	DatabaseConditionUpdating string = "Updating"
	DatabaseConditionUpdated  string = "Updated"
)

const (
	DatabaseConditionReasonCreating string = "CreatingDatabase"
	DatabaseConditionReasonCreated  string = "CreatedDatabase"
	DatabaseConditionReasonError    string = "ErroredDatabase"
	DatabaseConditionReasonUpdating string = "UpdatingDatabase"
	DatabaseConditionReasonUpdated  string = "UpdatedDatabase"
)

func (d *Database) CreatingCondition() *metav1.Condition {
	return &metav1.Condition{Type: DatabaseConditionCreating, Status: metav1.ConditionTrue,
		Reason: DatabaseConditionReasonCreating, Message: "Database is creating"}
}

func (d *Database) CreatedCondition() *metav1.Condition {
	return &metav1.Condition{Type: DatabaseConditionCreated, Status: metav1.ConditionTrue,
		Reason: DatabaseConditionReasonCreated, Message: "Database successfully created"}
}

func (d *Database) ErroredCondition() *metav1.Condition {
	return &metav1.Condition{Type: DatabaseConditionError, Status: metav1.ConditionTrue,
		Reason: DatabaseConditionReasonError, Message: "Database is erroring"}
}

func (d *Database) UpdatingCondition() *metav1.Condition {
	return &metav1.Condition{Type: DatabaseConditionUpdating, Status: metav1.ConditionTrue,
		Reason: DatabaseConditionReasonUpdating, Message: "Database is updating"}
}

func (d *Database) UpdatedCondition() *metav1.Condition {
	return &metav1.Condition{Type: DatabaseConditionUpdated, Status: metav1.ConditionTrue,
		Reason: DatabaseConditionReasonUpdated, Message: "Database successfully updated"}
}
