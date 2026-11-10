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
	"go.temporal.io/server/common/primitives"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func newCluster(t *testing.T) *v1beta1.TemporalCluster {
	t.Helper()

	cluster := &v1beta1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1beta1.TemporalClusterSpec{
			Version:          version.MustNewVersionFromString("1.26.2"),
			NumHistoryShards: 1,
			Persistence: v1beta1.TemporalPersistenceSpec{
				DefaultStore:    &v1beta1.DatastoreSpec{},
				VisibilityStore: &v1beta1.DatastoreSpec{},
			},
		},
	}
	cluster.Default()

	return cluster
}

func buildService(t *testing.T, cluster *v1beta1.TemporalCluster, service primitives.ServiceName) *appsv1.Deployment {
	t.Helper()

	spec, err := cluster.Spec.Services.GetServiceSpec(service)
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	builder := base.NewDeploymentBuilder(string(service), cluster, scheme, spec, "hash-1")
	object := builder.Build()
	require.NoError(t, builder.Update(object))

	deployment, ok := object.(*appsv1.Deployment)
	require.True(t, ok)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)

	return deployment
}

// TestDefaultLivenessProbe asserts every service gets a liveness probe, and that the
// worker — which serves no gRPC — is probed on its membership port rather than the rpc
// port the other services expose.
func TestDefaultLivenessProbe(t *testing.T) {
	cluster := newCluster(t)

	t.Run("frontend probes its rpc port", func(t *testing.T) {
		probe := buildService(t, cluster, primitives.FrontendService).Spec.Template.Spec.Containers[0].LivenessProbe
		require.NotNil(t, probe)
		require.NotNil(t, probe.TCPSocket)
		assert.Equal(t, intstr.FromString("rpc"), probe.TCPSocket.Port)
	})

	t.Run("worker probes its membership port", func(t *testing.T) {
		probe := buildService(t, cluster, primitives.WorkerService).Spec.Template.Spec.Containers[0].LivenessProbe
		require.NotNil(t, probe, "the worker must now carry a liveness probe")
		require.NotNil(t, probe.TCPSocket)
		assert.Equal(t, intstr.FromString("membership"), probe.TCPSocket.Port,
			"the worker has no rpc endpoint, so the probe must not target it")
		// The worker's probe has to be conservative: a worker that answers slowly is not a
		// worker that needs restarting.
		assert.GreaterOrEqual(t, probe.PeriodSeconds, int32(30))
		assert.GreaterOrEqual(t, probe.FailureThreshold, int32(5))
	})
}

// TestLivenessProbeOverride asserts spec.services.<name>.livenessProbe can replace or
// disable the probe the operator generates by default.
func TestLivenessProbeOverride(t *testing.T) {
	custom := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{"true"}},
		},
	}

	t.Run("custom probe replaces the default", func(t *testing.T) {
		cluster := newCluster(t)
		cluster.Spec.Services.Frontend.LivenessProbe = &v1beta1.LivenessProbeSpec{Probe: custom}

		probe := buildService(t, cluster, primitives.FrontendService).Spec.Template.Spec.Containers[0].LivenessProbe
		assert.Equal(t, custom, probe)
	})

	t.Run("disabled removes the probe", func(t *testing.T) {
		cluster := newCluster(t)
		cluster.Spec.Services.Worker.LivenessProbe = &v1beta1.LivenessProbeSpec{Disabled: true}

		probe := buildService(t, cluster, primitives.WorkerService).Spec.Template.Spec.Containers[0].LivenessProbe
		assert.Nil(t, probe)
	})
}
