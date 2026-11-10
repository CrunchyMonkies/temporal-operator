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

package ui_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/ui"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newClusterWithUI(t *testing.T) *v1beta1.TemporalCluster {
	t.Helper()

	cluster := &v1beta1.TemporalCluster{
		TypeMeta:   v1beta1.TemporalClusterTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1beta1.TemporalClusterSpec{
			Version:          version.MustNewVersionFromString("1.31.1"),
			NumHistoryShards: 1,
			UI:               &v1beta1.TemporalUISpec{Enabled: true},
		},
	}
	cluster.Default()

	return cluster
}

func buildUIService(t *testing.T, cluster *v1beta1.TemporalCluster, existing client.Object) *corev1.Service {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	builder := ui.NewServiceBuilder(cluster, scheme)

	object := existing
	if object == nil {
		object = builder.Build()
	}

	require.NoError(t, builder.Update(object))

	return object.(*corev1.Service)
}

func TestServiceBuilder(t *testing.T) {
	t.Run("defaults to a ClusterIP service", func(tt *testing.T) {
		service := buildUIService(tt, newClusterWithUI(tt), nil)

		assert.Equal(tt, corev1.ServiceTypeClusterIP, service.Spec.Type)
		require.Len(tt, service.Spec.Ports, 1)
		assert.Equal(tt, int32(ui.UIServicePort), service.Spec.Ports[0].Port)
		assert.Equal(tt, int32(0), service.Spec.Ports[0].NodePort)
	})

	t.Run("applies the requested type, annotations and node port", func(tt *testing.T) {
		cluster := newClusterWithUI(tt)
		cluster.Spec.UI.Service = &v1beta1.ServiceResourceSpec{
			ObjectMetaOverride: v1beta1.ObjectMetaOverride{
				Annotations: map[string]string{"a": "b"},
				Labels:      map[string]string{"c": "d"},
			},
			Type:     ptr.To(corev1.ServiceTypeNodePort),
			NodePort: ptr.To(int32(32080)),
		}

		service := buildUIService(tt, cluster, nil)

		assert.Equal(tt, corev1.ServiceTypeNodePort, service.Spec.Type)
		assert.Equal(tt, "b", service.Annotations["a"])
		assert.Equal(tt, "d", service.Labels["c"])
		require.Len(tt, service.Spec.Ports, 1)
		assert.Equal(tt, int32(32080), service.Spec.Ports[0].NodePort)
	})

	t.Run("doesn't revert fields set on the live service", func(tt *testing.T) {
		cluster := newClusterWithUI(tt)
		cluster.Spec.UI.Service = &v1beta1.ServiceResourceSpec{
			Type: ptr.To(corev1.ServiceTypeLoadBalancer),
		}

		service := buildUIService(tt, cluster, nil)
		service.Spec.LoadBalancerSourceRanges = []string{"192.168.0.0/16"}
		service.Spec.Ports[0].NodePort = 31080

		service = buildUIService(tt, cluster, service)

		assert.Equal(tt, corev1.ServiceTypeLoadBalancer, service.Spec.Type)
		assert.Equal(tt, []string{"192.168.0.0/16"}, service.Spec.LoadBalancerSourceRanges)
		assert.Equal(tt, int32(31080), service.Spec.Ports[0].NodePort)
	})
}
