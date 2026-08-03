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

package meta

import (
	"strings"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/metadata"
	"github.com/alexandrevilain/temporal-operator/internal/resource/mtls/istio"
	"github.com/alexandrevilain/temporal-operator/internal/resource/mtls/linkerd"
	"github.com/alexandrevilain/temporal-operator/internal/resource/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	configHashKey = "operator.temporal.io/config"
)

// managedLabelPrefixes and managedAnnotationPrefixes list the pod-template
// metadata namespaces this operator computes from the cluster spec.
//
// Keys under these prefixes are dropped from the existing pod template before
// the freshly computed metadata is overlaid. This is what makes removal work:
// the feature helpers (istio, linkerd, prometheus) return an *empty* map when
// their feature is disabled rather than a removal signal, so a plain merge over
// the existing metadata would make every operator-managed key permanent. For
// example, clearing spec.mTLS after using the istio provider would leave
// `sidecar.istio.io/inject: "true"` behind and istio would keep injecting
// sidecars forever.
//
// Anything outside these namespaces is preserved untouched, which is the point
// of merging with the existing metadata at all: annotations written by other
// actors (notably `kubectl.kubernetes.io/restartedAt` from `kubectl rollout
// restart`) must survive reconciliation.
var (
	managedLabelPrefixes = []string{
		"app.kubernetes.io/", // metadata.GetLabels
		"sidecar.istio.io/",  // istio.GetLabels
	}

	managedAnnotationPrefixes = []string{
		"linkerd.io/",           // linkerd.GetAnnotations
		"proxy.istio.io/",       // istio.GetAnnotations
		"prometheus.io/",        // prometheus.GetAnnotations
		"operator.temporal.io/", // configHashKey
	}
)

// dropManaged returns a copy of m without the keys this operator computes, so
// that stale ones do not survive into the merged result.
func dropManaged(m map[string]string, prefixes []string) map[string]string {
	return metadata.FilterAnnotations(m, func(k, _ string) bool {
		for _, prefix := range prefixes {
			if strings.HasPrefix(k, prefix) {
				return false
			}
		}
		return true
	})
}

// BuildPodObjectMeta return ObjectMeta for the service (frontend, ui, admintools) of the provided Cluster.
// It merges existing pod template labels and annotations with the operator-managed ones,
// so that externally-added annotations (e.g. from kubectl rollout restart) are preserved.
//
// Note: labels and annotations copied from the TemporalCluster's own metadata
// are re-derived from the spec on every call, so they track additions and edits
// there. Removing one from the TemporalCluster does not remove it from existing
// pod templates unless it falls under a managed prefix, since nothing records
// which keys a previous reconcile propagated.
func BuildPodObjectMeta(instance *v1beta1.TemporalCluster, service, configHash string, existing metav1.ObjectMeta) metav1.ObjectMeta {
	instanceAnnotations := metadata.FilterAnnotations(instance.Annotations, func(k, _ string) bool {
		return k != "kubectl.kubernetes.io/last-applied-configuration"
	})

	return metav1.ObjectMeta{
		Labels: metadata.Merge(
			dropManaged(existing.Labels, managedLabelPrefixes),
			istio.GetLabels(instance),
			metadata.GetLabels(instance, service, instance.Spec.Version, instance.Labels),
		),
		Annotations: metadata.Merge(
			dropManaged(existing.Annotations, managedAnnotationPrefixes),
			linkerd.GetAnnotations(instance),
			istio.GetAnnotations(instance),
			prometheus.GetAnnotations(instance),
			metadata.GetAnnotations(instance.Name, instanceAnnotations),
			map[string]string{
				configHashKey: configHash,
			},
		),
	}
}
