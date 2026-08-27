package v1beta1

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ReconcileErrorCondition indicates a transient or persistent reconciliation error.
	ReconcileErrorCondition string = "ReconcileError"
	// ReconcileSuccessCondition indicates a successful reconciliation.
	ReconcileSuccessCondition string = "ReconcileSuccess"
	// ReadyCondition indicates the cluster is ready to receive traffic.
	ReadyCondition string = "Ready"
)

const (
	// ProgressingReason signals a reconciliation has started.
	ProgressingReason string = "Progressing"
	// ReconcileErrorReason signals a unknown reconciliation error.
	ReconcileErrorReason string = "LastReconcileCycleFailed"
	// SpecValidationFailedReason signals a reconciliation error the spec itself causes, which no
	// retry can clear. The controller stops retrying and waits for the spec to change.
	SpecValidationFailedReason string = "SpecValidationFailed"
	// ReconcileSuccessReason signals a successful reconciliation.
	ReconcileSuccessReason string = "LastReconcileCycleSucceded"
	// ServicesReadyReason signals all temporal services for the cluster are in ready state.
	ServicesReadyReason string = "ServicesReady"
	// ServicesNotReadyReason signals that not all temporal services for the cluster are in ready state.
	ServicesNotReadyReason string = "ServicesNotReady"
	// PersistenceReconciliationFailedReason signals an error while reconciling persistence.
	PersistenceReconciliationFailedReason string = "PersistenceReconciliationFailed"
	// ResourcesReconciliationFailedReason signals an error while reconciling cluster resources.
	ResourcesReconciliationFailedReason string = "ResoucesReconciliationFailed"
	// TemporalClusterValidationFailedReason signals an error while validation desired cluster version.
	TemporalClusterValidationFailedReason string = "TemporalClusterValidationFailed"
	// TemporalNamespaceCreatedReason signals a successful namespace creation.
	TemporalNamespaceCreatedReason string = "TemporalNamespaceCreated"
	// TemporalScheduleCreatedReason signals a successful schedule creation.
	TemporalScheduleCreatedReason string = "TemporalScheduleCreated"
	// TargetClusterReachableReason signals the operator connected to a target cluster.
	TargetClusterReachableReason string = "TargetClusterReachable"
	// TargetClusterUnreachableReason signals the operator could not connect to a target cluster.
	TargetClusterUnreachableReason string = "TargetClusterUnreachable"
	// TargetClusterResolutionFailedReason signals an error while resolving the target cluster a
	// resource references.
	TargetClusterResolutionFailedReason string = "TargetClusterResolutionFailed"
)

// SetTemporalClusterReconcileSuccess sets the ReconcileSuccessCondition status for a temporal cluster.
func SetTemporalClusterReconcileSuccess(c *TemporalCluster, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReconcileSuccessCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: c.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&c.Status.Conditions, condition)
}

// SetTemporalClusterReconcileError sets the ReconcileErrorCondition status for a temporal cluster.
func SetTemporalClusterReconcileError(c *TemporalCluster, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReconcileErrorCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: c.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&c.Status.Conditions, condition)
}

// GetTemporalClusterReadyCondition returns the ready condition for the provided cluster if found.
func GetTemporalClusterReadyCondition(c *TemporalCluster) (*metav1.Condition, bool) {
	condition := apimeta.FindStatusCondition(c.Status.Conditions, ReadyCondition)
	return condition, condition != nil
}

// SetTemporalClusterReady sets the ReadyCondition status for a temporal cluster.
func SetTemporalClusterReady(c *TemporalCluster, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReadyCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: c.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&c.Status.Conditions, condition)
}

// SetTemporalNamespaceReady sets the ReadyCondition status for a temporal namespace.
func SetTemporalNamespaceReady(c *TemporalNamespace, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReadyCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: c.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&c.Status.Conditions, condition)
}

// SetTemporalScheduleReady sets the ReadyCondition status for a temporal schedule.
func SetTemporalScheduleReady(s *TemporalSchedule, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReadyCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: s.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&s.Status.Conditions, condition)
}

// SetTemporalTargetClusterReady sets the ReadyCondition status for a temporal target cluster.
func SetTemporalTargetClusterReady(c *TemporalTargetCluster, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReadyCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: c.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&c.Status.Conditions, condition)
}

// SetTemporalNamespaceReconcileSuccess sets the ReconcileSuccessCondition status for a temporal namespace.
func SetTemporalNamespaceReconcileSuccess(n *TemporalNamespace, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReconcileSuccessCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: n.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&n.Status.Conditions, condition)
}

// SetTemporalScheduleReconcileSuccess sets the ReconcileSuccessCondition status for a temporal schedule.
func SetTemporalScheduleReconcileSuccess(s *TemporalSchedule, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReconcileSuccessCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: s.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&s.Status.Conditions, condition)
}

// SetTemporalNamespaceReconcileError sets the ReconcileErrorCondition status for a temporal namespace.
func SetTemporalNamespaceReconcileError(n *TemporalNamespace, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReconcileErrorCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: n.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&n.Status.Conditions, condition)
}

// SetTemporalScheduleReconcileError sets the ReconcileErrorCondition status for a temporal schedule.
func SetTemporalScheduleReconcileError(s *TemporalSchedule, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               ReconcileErrorCondition,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: s.GetGeneration(),
		Reason:             reason,
		Status:             status,
		Message:            message,
	}
	apimeta.SetStatusCondition(&s.Status.Conditions, condition)
}
