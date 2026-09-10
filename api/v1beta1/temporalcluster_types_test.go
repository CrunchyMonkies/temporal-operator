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

package v1beta1_test

import (
	"testing"
	"time"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestClientAddresses(t *testing.T) {
	tests := map[string]struct {
		cluster *v1beta1.TemporalCluster
		// expectedPublic is the address the temporal services themselves are configured with. It
		// always has to stay resolvable from inside the cluster hosting them.
		expectedPublic string
		// expectedOperator is the address the operator dials.
		expectedOperator string
	}{
		"frontend": {
			cluster: cluster(v1beta1.TemporalClusterSpec{
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{Port: ptr.To(int32(7233))},
				},
			}),
			expectedPublic:   "test-frontend.demo:7233",
			expectedOperator: "test-frontend.demo:7233",
		},
		"internal frontend": {
			cluster: cluster(v1beta1.TemporalClusterSpec{
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{Port: ptr.To(int32(7233))},
					InternalFrontend: &v1beta1.InternalFrontendServiceSpec{
						ServiceSpec: v1beta1.ServiceSpec{Port: ptr.To(int32(7236))},
						Enabled:     true,
					},
				},
			}),
			expectedPublic:   "test-internal-frontend-headless.demo:7236",
			expectedOperator: "test-internal-frontend-headless.demo:7236",
		},
		"mTLS frontend": {
			cluster: cluster(v1beta1.TemporalClusterSpec{
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{Port: ptr.To(int32(7233))},
				},
				MTLS: &v1beta1.MTLSSpec{
					Provider: v1beta1.CertManagerMTLSProvider,
					Frontend: &v1beta1.FrontendMTLSSpec{Enabled: true},
				},
			}),
			expectedPublic:   "test-frontend.demo:7233",
			expectedOperator: "test-frontend.demo:7233",
		},
		"override": {
			cluster: cluster(v1beta1.TemporalClusterSpec{
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{Port: ptr.To(int32(7233))},
				},
				OperatorClientAddress: "temporal.example.com:7233",
			}),
			expectedPublic:   "test-frontend.demo:7233",
			expectedOperator: "temporal.example.com:7233",
		},
		"override with internal frontend enabled": {
			cluster: cluster(v1beta1.TemporalClusterSpec{
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{Port: ptr.To(int32(7233))},
					InternalFrontend: &v1beta1.InternalFrontendServiceSpec{
						ServiceSpec: v1beta1.ServiceSpec{Port: ptr.To(int32(7236))},
						Enabled:     true,
					},
				},
				OperatorClientAddress: "temporal.example.com:7233",
			}),
			expectedPublic:   "test-internal-frontend-headless.demo:7236",
			expectedOperator: "temporal.example.com:7233",
		},
		"override with mTLS frontend": {
			cluster: cluster(v1beta1.TemporalClusterSpec{
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{Port: ptr.To(int32(7233))},
				},
				MTLS: &v1beta1.MTLSSpec{
					Provider: v1beta1.CertManagerMTLSProvider,
					Frontend: &v1beta1.FrontendMTLSSpec{Enabled: true},
				},
				OperatorClientAddress: "temporal.example.com:7233",
			}),
			expectedPublic:   "test-frontend.demo:7233",
			expectedOperator: "temporal.example.com:7233",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expectedPublic, test.cluster.GetPublicClientAddress())
			assert.Equal(t, test.expectedOperator, test.cluster.GetOperatorClientAddress())
		})
	}
}

func cluster(spec v1beta1.TemporalClusterSpec) *v1beta1.TemporalCluster {
	return &v1beta1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "demo"},
		Spec:       spec,
	}
}

func TestTargetClusterDefaults(t *testing.T) {
	t.Run("kubeconfig key falls back when unset", func(t *testing.T) {
		assert.Equal(t, "kubeconfig", (&v1beta1.TemporalTargetCluster{}).GetKey())
		assert.Equal(t, "config", (&v1beta1.TemporalTargetCluster{
			Spec: v1beta1.TemporalTargetClusterSpec{Key: "config"},
		}).GetKey())
	})

	t.Run("resync period only applies to Resync drift detection", func(t *testing.T) {
		watching := &v1beta1.TemporalTargetCluster{
			Spec: v1beta1.TemporalTargetClusterSpec{
				DriftDetection: v1beta1.WatchTargetClusterDriftDetection,
				ResyncPeriod:   &metav1.Duration{Duration: time.Minute},
			},
		}
		assert.Zero(t, watching.GetResyncPeriod())

		defaulted := &v1beta1.TemporalTargetCluster{
			Spec: v1beta1.TemporalTargetClusterSpec{
				DriftDetection: v1beta1.ResyncTargetClusterDriftDetection,
			},
		}
		assert.Equal(t, v1beta1.DefaultTargetClusterResyncPeriod, defaulted.GetResyncPeriod())

		explicit := &v1beta1.TemporalTargetCluster{
			Spec: v1beta1.TemporalTargetClusterSpec{
				DriftDetection: v1beta1.ResyncTargetClusterDriftDetection,
				ResyncPeriod:   &metav1.Duration{Duration: time.Minute},
			},
		}
		assert.Equal(t, time.Minute, explicit.GetResyncPeriod())
	})
}

func TestMaintenanceSpec(t *testing.T) {
	tests := map[string]struct {
		maintenance              *v1beta1.MaintenanceSpec
		expectedEnabled          bool
		expectedScalesUI         bool
		expectedScalesAdminTools bool
	}{
		"unset": {
			maintenance: nil,
		},
		"disabled": {
			maintenance: &v1beta1.MaintenanceSpec{Enabled: false},
		},
		"disabled with the extras opted in": {
			// Nothing scales down while maintenance mode is off, whatever the extras say.
			maintenance: &v1beta1.MaintenanceSpec{
				Enabled:           false,
				IncludeUI:         ptr.To(true),
				IncludeAdminTools: ptr.To(true),
			},
		},
		"enabled with defaults": {
			maintenance:      &v1beta1.MaintenanceSpec{Enabled: true},
			expectedEnabled:  true,
			expectedScalesUI: true,
		},
		"enabled keeping the ui up": {
			maintenance: &v1beta1.MaintenanceSpec{
				Enabled:   true,
				IncludeUI: ptr.To(false),
			},
			expectedEnabled: true,
		},
		"enabled taking the admin tools down too": {
			maintenance: &v1beta1.MaintenanceSpec{
				Enabled:           true,
				IncludeAdminTools: ptr.To(true),
			},
			expectedEnabled:          true,
			expectedScalesUI:         true,
			expectedScalesAdminTools: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expectedEnabled, test.maintenance.IsEnabled())
			assert.Equal(t, test.expectedScalesUI, test.maintenance.ScalesDownUI())
			assert.Equal(t, test.expectedScalesAdminTools, test.maintenance.ScalesDownAdminTools())
		})
	}
}
