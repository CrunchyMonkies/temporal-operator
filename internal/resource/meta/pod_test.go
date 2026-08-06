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

package meta_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/meta"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestCluster() *v1beta1.TemporalCluster {
	return &v1beta1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: v1beta1.TemporalClusterSpec{
			Version: version.MustNewVersionFromString("1.24.1"),
		},
	}
}

func TestBuildPodObjectMeta_PreservesExistingAnnotations(t *testing.T) {
	instance := newTestCluster()
	existing := metav1.ObjectMeta{
		Annotations: map[string]string{
			"kubectl.kubernetes.io/restartedAt": "2024-01-01T00:00:00Z",
			"custom-annotation":                 "custom-value",
		},
	}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	assert.Equal(t, "2024-01-01T00:00:00Z", result.Annotations["kubectl.kubernetes.io/restartedAt"])
	assert.Equal(t, "custom-value", result.Annotations["custom-annotation"])
	assert.Equal(t, "abc123", result.Annotations["operator.temporal.io/config"])
}

func TestBuildPodObjectMeta_PreservesExistingLabels(t *testing.T) {
	instance := newTestCluster()
	existing := metav1.ObjectMeta{
		Labels: map[string]string{
			"custom-label": "custom-value",
		},
	}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	assert.Equal(t, "custom-value", result.Labels["custom-label"])
	assert.Equal(t, "test-cluster", result.Labels["app.kubernetes.io/name"])
	assert.Equal(t, "frontend", result.Labels["app.kubernetes.io/component"])
}

func TestBuildPodObjectMeta_OperatorAnnotationsOverrideExisting(t *testing.T) {
	instance := newTestCluster()
	existing := metav1.ObjectMeta{
		Annotations: map[string]string{
			"operator.temporal.io/config": "old-hash",
		},
	}

	result := meta.BuildPodObjectMeta(instance, "frontend", "new-hash", existing)

	assert.Equal(t, "new-hash", result.Annotations["operator.temporal.io/config"])
}

func TestBuildPodObjectMeta_EmptyExistingObjectMeta(t *testing.T) {
	instance := newTestCluster()
	existing := metav1.ObjectMeta{}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	assert.Equal(t, "abc123", result.Annotations["operator.temporal.io/config"])
	assert.Equal(t, "test-cluster", result.Labels["app.kubernetes.io/name"])
	assert.Equal(t, "frontend", result.Labels["app.kubernetes.io/component"])
}

// The tests above cover the additive direction. The ones below cover removal:
// the mTLS/metrics helpers return an empty map when their feature is disabled
// rather than a removal signal, so merging over the existing pod template must
// not make operator-managed keys permanent.

func TestBuildPodObjectMeta_RemovesIstioMetadataWhenMTLSCleared(t *testing.T) {
	instance := newTestCluster()
	// mTLS is not set on the instance, but the live pod template still carries
	// the istio metadata from when it was.
	existing := metav1.ObjectMeta{
		Labels: map[string]string{
			"sidecar.istio.io/inject": "true",
			"custom-label":            "custom-value",
		},
		Annotations: map[string]string{
			"proxy.istio.io/config": `{ "holdApplicationUntilProxyStarts": true }`,
			"custom-annotation":     "custom-value",
		},
	}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	assert.NotContains(t, result.Labels, "sidecar.istio.io/inject")
	assert.NotContains(t, result.Annotations, "proxy.istio.io/config")
	// Metadata the operator does not manage is still preserved.
	assert.Equal(t, "custom-value", result.Labels["custom-label"])
	assert.Equal(t, "custom-value", result.Annotations["custom-annotation"])
}

func TestBuildPodObjectMeta_RemovesLinkerdAnnotationWhenMTLSCleared(t *testing.T) {
	instance := newTestCluster()
	existing := metav1.ObjectMeta{
		Annotations: map[string]string{
			"linkerd.io/inject": "enabled",
		},
	}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	assert.NotContains(t, result.Annotations, "linkerd.io/inject")
}

func TestBuildPodObjectMeta_RemovesPrometheusAnnotationsWhenMetricsDisabled(t *testing.T) {
	instance := newTestCluster()
	existing := metav1.ObjectMeta{
		Annotations: map[string]string{
			"prometheus.io/scrape": "true",
			"prometheus.io/scheme": "http",
			"prometheus.io/path":   "/metrics",
			"prometheus.io/port":   "9090",
		},
	}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	for _, key := range []string{"prometheus.io/scrape", "prometheus.io/scheme", "prometheus.io/path", "prometheus.io/port"} {
		assert.NotContains(t, result.Annotations, key)
	}
}

func TestBuildPodObjectMeta_KeepsIstioMetadataWhileMTLSEnabled(t *testing.T) {
	instance := newTestCluster()
	instance.Spec.MTLS = &v1beta1.MTLSSpec{Provider: v1beta1.IstioMTLSProvider}
	existing := metav1.ObjectMeta{}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	// Stripping managed keys must not defeat the feature while it is enabled:
	// the value is recomputed from the spec on every call.
	assert.Equal(t, "true", result.Labels["sidecar.istio.io/inject"])
	assert.Contains(t, result.Annotations, "proxy.istio.io/config")
}

func TestBuildPodObjectMeta_RemovesStaleVersionLabel(t *testing.T) {
	instance := newTestCluster()
	existing := metav1.ObjectMeta{
		Labels: map[string]string{
			"app.kubernetes.io/version": "1.20.0",
		},
	}

	result := meta.BuildPodObjectMeta(instance, "frontend", "abc123", existing)

	assert.Equal(t, "1.24.1", result.Labels["app.kubernetes.io/version"])
}
