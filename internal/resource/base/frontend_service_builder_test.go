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

package base_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/base"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newCluster(t *testing.T) *v1beta1.TemporalCluster {
	t.Helper()

	cluster := &v1beta1.TemporalCluster{
		TypeMeta:   v1beta1.TemporalClusterTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1beta1.TemporalClusterSpec{
			Version:          version.MustNewVersionFromString("1.31.1"),
			NumHistoryShards: 1,
		},
	}
	cluster.Default()

	return cluster
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	return scheme
}

func buildFrontendService(t *testing.T, cluster *v1beta1.TemporalCluster, existing client.Object) *corev1.Service {
	t.Helper()

	builder := base.NewFrontendServiceBuilder(cluster, newScheme(t))

	object := existing
	if object == nil {
		object = builder.Build()
	}

	require.NoError(t, builder.Update(object))

	return object.(*corev1.Service)
}

func TestFrontendServiceBuilder(t *testing.T) {
	t.Run("defaults to a ClusterIP service", func(tt *testing.T) {
		service := buildFrontendService(tt, newCluster(tt), nil)

		assert.Equal(tt, corev1.ServiceTypeClusterIP, service.Spec.Type)
		require.Len(tt, service.Spec.Ports, 2)
		assert.Equal(tt, "grpc-rpc", service.Spec.Ports[0].Name)
		assert.Equal(tt, int32(0), service.Spec.Ports[0].NodePort)
	})

	t.Run("applies the requested type, annotations and node port", func(tt *testing.T) {
		cluster := newCluster(tt)
		cluster.Spec.Services.Frontend.Service = &v1beta1.ServiceResourceSpec{
			ObjectMetaOverride: v1beta1.ObjectMetaOverride{
				Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-internal": "true"},
				Labels:      map[string]string{"exposed": "true"},
			},
			Type:     ptr.To(corev1.ServiceTypeLoadBalancer),
			NodePort: ptr.To(int32(32233)),
		}

		service := buildFrontendService(tt, cluster, nil)

		assert.Equal(tt, corev1.ServiceTypeLoadBalancer, service.Spec.Type)
		assert.Equal(tt, "true", service.Annotations["service.beta.kubernetes.io/aws-load-balancer-internal"])
		assert.Equal(tt, "true", service.Labels["exposed"])
		require.Len(tt, service.Spec.Ports, 2)
		assert.Equal(tt, "grpc-rpc", service.Spec.Ports[0].Name)
		assert.Equal(tt, int32(32233), service.Spec.Ports[0].NodePort)
		// The node port only applies to the gRPC port.
		assert.Equal(tt, int32(0), service.Spec.Ports[1].NodePort)
	})

	t.Run("doesn't revert fields set on the live service", func(tt *testing.T) {
		cluster := newCluster(tt)
		cluster.Spec.Services.Frontend.Service = &v1beta1.ServiceResourceSpec{
			Type: ptr.To(corev1.ServiceTypeLoadBalancer),
		}

		// First reconciliation, then simulate both the api server allocating a node port
		// and the user setting fields the operator doesn't manage.
		service := buildFrontendService(tt, cluster, nil)
		service.Spec.LoadBalancerSourceRanges = []string{"10.0.0.0/8"}
		service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
		service.Spec.Ports[0].NodePort = 31234

		service = buildFrontendService(tt, cluster, service)

		assert.Equal(tt, corev1.ServiceTypeLoadBalancer, service.Spec.Type)
		assert.Equal(tt, []string{"10.0.0.0/8"}, service.Spec.LoadBalancerSourceRanges)
		assert.Equal(tt, corev1.ServiceExternalTrafficPolicyTypeLocal, service.Spec.ExternalTrafficPolicy)
		assert.Equal(tt, int32(31234), service.Spec.Ports[0].NodePort)
	})

	t.Run("keeps a type set outside of the spec", func(tt *testing.T) {
		cluster := newCluster(tt)

		service := buildFrontendService(tt, cluster, nil)
		service.Spec.Type = corev1.ServiceTypeNodePort

		service = buildFrontendService(tt, cluster, service)

		assert.Equal(tt, corev1.ServiceTypeNodePort, service.Spec.Type)
	})
}
