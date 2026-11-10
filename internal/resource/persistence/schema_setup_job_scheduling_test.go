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

package persistence_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/persistence"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func newSchemaJobCluster(t *testing.T, scheduling *v1beta1.JobSchedulingSpec) *v1beta1.TemporalCluster {
	t.Helper()

	store := func() *v1beta1.DatastoreSpec {
		return &v1beta1.DatastoreSpec{
			SQL: &v1beta1.SQLSpec{
				User:            "temporal",
				PluginName:      "postgres12",
				DatabaseName:    "temporal",
				ConnectAddr:     "postgres:5432",
				ConnectProtocol: "tcp",
			},
		}
	}

	cluster := &v1beta1.TemporalCluster{
		TypeMeta:   v1beta1.TemporalClusterTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1beta1.TemporalClusterSpec{
			Version:          version.MustNewVersionFromString("1.26.2"),
			NumHistoryShards: 1,
			JobScheduling:    scheduling,
			Persistence: v1beta1.TemporalPersistenceSpec{
				DefaultStore:    store(),
				VisibilityStore: store(),
			},
		},
	}
	cluster.Spec.Persistence.DefaultStore.Name = "default"
	cluster.Spec.Persistence.VisibilityStore.Name = "visibility"
	cluster.Default()

	return cluster
}

func buildSchemaJob(t *testing.T, cluster *v1beta1.TemporalCluster) *batchv1.Job {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	builder := persistence.NewSchemaJobBuilder(cluster, scheme, "setup-default-schema", []string{"/etc/scripts/setup-default-schema.sh"})
	job, ok := builder.Build().(*batchv1.Job)
	require.True(t, ok)
	require.NoError(t, builder.Update(job))

	return job
}

// TestSchemaJobBuilderScheduling asserts spec.jobScheduling is reported on the pods
// of the jobs the operator creates, so they can be scheduled on tainted or dedicated nodes.
func TestSchemaJobBuilderScheduling(t *testing.T) {
	tolerations := []corev1.Toleration{
		{
			Key:      "dedicated",
			Operator: corev1.TolerationOpEqual,
			Value:    "temporal",
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}

	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/os",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"linux"},
							},
						},
					},
				},
			},
		},
	}

	tests := map[string]struct {
		scheduling          *v1beta1.JobSchedulingSpec
		expectedTolerations []corev1.Toleration
		expectedAffinity    *corev1.Affinity
	}{
		"unset": {
			scheduling: nil,
		},
		"empty": {
			scheduling: &v1beta1.JobSchedulingSpec{},
		},
		"tolerations only": {
			scheduling:          &v1beta1.JobSchedulingSpec{Tolerations: tolerations},
			expectedTolerations: tolerations,
		},
		"affinity only": {
			scheduling:       &v1beta1.JobSchedulingSpec{Affinity: affinity},
			expectedAffinity: affinity,
		},
		"tolerations and affinity": {
			scheduling:          &v1beta1.JobSchedulingSpec{Tolerations: tolerations, Affinity: affinity},
			expectedTolerations: tolerations,
			expectedAffinity:    affinity,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			job := buildSchemaJob(t, newSchemaJobCluster(t, test.scheduling))

			assert.Equal(t, test.expectedTolerations, job.Spec.Template.Spec.Tolerations)
			assert.Equal(t, test.expectedAffinity, job.Spec.Template.Spec.Affinity)
		})
	}
}

// TestJobSchedulingSpecAccessorsAreNilSafe asserts the accessors used by the job builder
// tolerate a cluster which doesn't set spec.jobScheduling at all.
func TestJobSchedulingSpecAccessorsAreNilSafe(t *testing.T) {
	var spec *v1beta1.JobSchedulingSpec

	assert.Nil(t, spec.GetTolerations())
	assert.Nil(t, spec.GetAffinity())
}
