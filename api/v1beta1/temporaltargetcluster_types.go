// Licensed to Alexandre VILAIN under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Alexandre VILAIN licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package v1beta1

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TargetClusterDriftDetection determines how the operator notices changes made to the resources
// it manages in a target cluster.
// +kubebuilder:validation:Enum=Watch;Resync
type TargetClusterDriftDetection string

const (
	// WatchTargetClusterDriftDetection makes the operator open informers on the target cluster, so
	// changes to managed resources are reconciled as they happen. It costs a cache and a set of
	// watch connections per target cluster.
	WatchTargetClusterDriftDetection TargetClusterDriftDetection = "Watch"
	// ResyncTargetClusterDriftDetection makes the operator re-reconcile on an interval instead of
	// watching. Cheaper, at the cost of correcting drift on the interval rather than immediately.
	ResyncTargetClusterDriftDetection TargetClusterDriftDetection = "Resync"
)

// DefaultTargetClusterResyncPeriod is how often resources are reconciled when drift detection is
// set to Resync and no period is provided.
const DefaultTargetClusterResyncPeriod = 5 * time.Minute

// TemporalTargetClusterSpec defines the desired state of TemporalTargetCluster.
type TemporalTargetClusterSpec struct {
	// KubeconfigSecretRef references a Secret in the same namespace as this resource, holding a
	// kubeconfig for the target cluster.
	KubeconfigSecretRef corev1.LocalObjectReference `json:"kubeconfigSecretRef"`
	// Key is the Secret key holding the kubeconfig.
	// +kubebuilder:default=kubeconfig
	// +kubebuilder:validation:Optional
	Key string `json:"key,omitempty"`
	// Context selects a context from the kubeconfig.
	// Defaults to the kubeconfig's current context if omitted.
	// +kubebuilder:validation:Optional
	Context string `json:"context,omitempty"`
	// DriftDetection determines how changes made to managed resources in this cluster are noticed.
	// +kubebuilder:default=Watch
	// +kubebuilder:validation:Optional
	DriftDetection TargetClusterDriftDetection `json:"driftDetection,omitempty"`
	// ResyncPeriod is how often managed resources are reconciled when DriftDetection is Resync.
	// Defaults to 5m. Ignored when DriftDetection is Watch.
	// +kubebuilder:validation:Optional
	ResyncPeriod *metav1.Duration `json:"resyncPeriod,omitempty"`
}

// TemporalTargetClusterStatus defines the observed state of TemporalTargetCluster.
type TemporalTargetClusterStatus struct {
	// ServerVersion is the kubernetes version reported by the target cluster.
	// +optional
	ServerVersion string `json:"serverVersion,omitempty"`
	// AvailableAPIs reports the optional APIs the operator detected in the target cluster. A
	// TemporalCluster relying on one that isn't present there can't be reconciled.
	// +optional
	AvailableAPIs *TargetClusterAvailableAPIs `json:"availableAPIs,omitempty"`
	// Conditions holds the current state of the connection to the target cluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TargetClusterAvailableAPIs reports which of the optional APIs the operator integrates with are
// served by a target cluster.
type TargetClusterAvailableAPIs struct {
	CertManager        bool `json:"certManager"`
	Istio              bool `json:"istio"`
	PrometheusOperator bool `json:"prometheusOperator"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Server version",type="string",JSONPath=".status.serverVersion",description="Kubernetes version of the target cluster"
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Whether the target cluster is reachable"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// A TemporalTargetCluster is a kubernetes cluster, other than the one the operator watches, that
// the operator can create Temporal resources in. TemporalClusters, TemporalNamespaces and
// TemporalSchedules reference one by name to have their resources created there instead of
// alongside themselves.
//
// Resources created in a target cluster can't carry an owner reference back to the custom resource
// that asked for them, as that resource lives in another cluster and the target's garbage
// collector would delete them as orphaned. They are labelled instead, and cleaned up through a
// finalizer.
type TemporalTargetCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemporalTargetClusterSpec   `json:"spec,omitempty"`
	Status TemporalTargetClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TemporalTargetClusterList contains a list of TemporalTargetCluster.
type TemporalTargetClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemporalTargetCluster `json:"items"`
}

// GetKey returns the Secret key holding the kubeconfig, defaulted for resources created before the
// field had a server-side default.
func (c *TemporalTargetCluster) GetKey() string {
	if c.Spec.Key == "" {
		return "kubeconfig"
	}
	return c.Spec.Key
}

// GetResyncPeriod returns how often resources managed in this cluster should be reconciled, or 0
// when drift is detected using watches instead.
func (c *TemporalTargetCluster) GetResyncPeriod() time.Duration {
	if c.Spec.DriftDetection != ResyncTargetClusterDriftDetection {
		return 0
	}

	if c.Spec.ResyncPeriod != nil {
		return c.Spec.ResyncPeriod.Duration
	}

	return DefaultTargetClusterResyncPeriod
}

func init() {
	SchemeBuilder.Register(&TemporalTargetCluster{}, &TemporalTargetClusterList{})
}
