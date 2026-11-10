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

package persistence

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func schemaJobCluster(v string, adminTools *v1beta1.TemporalAdminToolsSpec) *v1beta1.TemporalCluster {
	return &v1beta1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "demo"},
		Spec: v1beta1.TemporalClusterSpec{
			Version:    version.MustNewVersionFromString(v),
			AdminTools: adminTools,
			Services: &v1beta1.ServicesSpec{
				Frontend: &v1beta1.ServiceSpec{Port: ptr.To[int32](7233)},
			},
			Persistence: v1beta1.TemporalPersistenceSpec{
				DefaultStore:    &v1beta1.DatastoreSpec{Name: "default"},
				VisibilityStore: &v1beta1.DatastoreSpec{Name: "visibility"},
			},
		},
	}
}

// TestSchemaJobBuilder_AdminToolsImage asserts that the schema setup job runs the
// admin tools image the user asked for through spec.admintools.{image,version},
// only falling back to the operator defaults when they're unset.
func TestSchemaJobBuilder_AdminToolsImage(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		adminTools *v1beta1.TemporalAdminToolsSpec
		expected   string
	}{
		{
			name:       "honors user-provided admintools version",
			version:    "1.24.2",
			adminTools: &v1beta1.TemporalAdminToolsSpec{Version: "1.24.2"},
			expected:   "temporalio/admin-tools:1.24.2",
		},
		{
			name:       "honors user-provided admintools image",
			version:    "1.26.2",
			adminTools: &v1beta1.TemporalAdminToolsSpec{Image: "registry.example.com/admin-tools", Version: "1.26.2-custom"},
			expected:   "registry.example.com/admin-tools:1.26.2-custom",
		},
		{
			name:       "falls back to the default tag when version is unset",
			version:    "1.26.2",
			adminTools: &v1beta1.TemporalAdminToolsSpec{Image: "registry.example.com/admin-tools"},
			expected:   "registry.example.com/admin-tools:1.26",
		},
		{
			name:       "falls back to the defaults when admintools is unset",
			version:    "1.24.2",
			adminTools: nil,
			expected:   "temporalio/admin-tools:1.24.2-tctl-1.18.1-cli-1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := schemaJobCluster(tt.version, tt.adminTools)

			job, ok := NewSchemaJobBuilder(cluster, nil, "setup-schema", []string{"/etc/scripts/setup-schema.sh"}).Build().(*batchv1.Job)
			require.True(t, ok)

			containers := job.Spec.Template.Spec.Containers
			require.Len(t, containers, 1)
			assert.Equal(t, tt.expected, containers[0].Image)
		})
	}
}

// TestAdminToolsImageSharedWithDeployment makes sure the job builder and the admin
// tools deployment resolve their image through the same helper, so they can't drift.
func TestSchemaJobBuilder_AdminToolsImageMatchesClusterHelper(t *testing.T) {
	cluster := schemaJobCluster("1.26.2", &v1beta1.TemporalAdminToolsSpec{Version: "1.26.2"})

	job, ok := NewSchemaJobBuilder(cluster, nil, "setup-schema", []string{"/etc/scripts/setup-schema.sh"}).Build().(*batchv1.Job)
	require.True(t, ok)

	assert.Equal(t, cluster.AdminToolsImage(), job.Spec.Template.Spec.Containers[0].Image)
}
