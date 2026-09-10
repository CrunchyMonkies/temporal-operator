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

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestPProfSpecAccessors(t *testing.T) {
	t.Run("nil spec is disabled and still reports defaults", func(t *testing.T) {
		var spec *v1beta1.PProfSpec
		assert.False(t, spec.IsEnabled())
		assert.Equal(t, v1beta1.DefaultPProfPort, spec.GetPort())
		assert.Equal(t, v1beta1.DefaultPProfHost, spec.GetHost())
	})

	t.Run("present but not enabled", func(t *testing.T) {
		assert.False(t, (&v1beta1.PProfSpec{}).IsEnabled())
	})

	t.Run("user values win over defaults", func(t *testing.T) {
		spec := &v1beta1.PProfSpec{Enabled: true, Port: ptr.To(int32(6060)), Host: "0.0.0.0"}
		assert.True(t, spec.IsEnabled())
		assert.Equal(t, int32(6060), spec.GetPort())
		assert.Equal(t, "0.0.0.0", spec.GetHost())
	})
}

func TestPProfDefaults(t *testing.T) {
	t.Run("disabled pprof is left untouched", func(t *testing.T) {
		c := cluster(v1beta1.TemporalClusterSpec{NumHistoryShards: 1})
		c.Default()
		assert.Nil(t, c.Spec.PProf)
	})

	t.Run("enabled pprof gets port and host defaults", func(t *testing.T) {
		c := cluster(v1beta1.TemporalClusterSpec{
			NumHistoryShards: 1,
			PProf:            &v1beta1.PProfSpec{Enabled: true},
		})
		c.Default()

		if assert.NotNil(t, c.Spec.PProf.Port) {
			assert.Equal(t, v1beta1.DefaultPProfPort, *c.Spec.PProf.Port)
		}
		assert.Equal(t, v1beta1.DefaultPProfHost, c.Spec.PProf.Host)
	})

	t.Run("explicit values are preserved", func(t *testing.T) {
		c := cluster(v1beta1.TemporalClusterSpec{
			NumHistoryShards: 1,
			PProf:            &v1beta1.PProfSpec{Enabled: true, Port: ptr.To(int32(6060)), Host: "0.0.0.0"},
		})
		c.Default()

		assert.Equal(t, int32(6060), *c.Spec.PProf.Port)
		assert.Equal(t, "0.0.0.0", c.Spec.PProf.Host)
	})

	t.Run("the default pprof port collides with nothing the operator opens", func(t *testing.T) {
		c := cluster(v1beta1.TemporalClusterSpec{
			NumHistoryShards: 1,
			PProf:            &v1beta1.PProfSpec{Enabled: true},
			Metrics: &v1beta1.MetricsSpec{
				Enabled:    true,
				Prometheus: &v1beta1.PrometheusSpec{},
			},
			Services: &v1beta1.ServicesSpec{
				InternalFrontend: &v1beta1.InternalFrontendServiceSpec{Enabled: true},
			},
		})
		c.Default()

		_, errs := c.Spec.ValidatePProf()
		assert.Empty(t, errs)
	})
}

func TestValidatePProf(t *testing.T) {
	// Every service port the operator assigns by default, so a collision test
	// fails loudly if a future default lands on the pprof port.
	defaulted := func(pprof *v1beta1.PProfSpec) *v1beta1.TemporalCluster {
		c := cluster(v1beta1.TemporalClusterSpec{
			NumHistoryShards: 1,
			PProf:            pprof,
			Services: &v1beta1.ServicesSpec{
				InternalFrontend: &v1beta1.InternalFrontendServiceSpec{Enabled: true},
			},
		})
		c.Default()
		return c
	}

	t.Run("disabled pprof is never validated", func(t *testing.T) {
		c := defaulted(&v1beta1.PProfSpec{Port: ptr.To(int32(7233))})
		_, errs := c.Spec.ValidatePProf()
		assert.Empty(t, errs)
	})

	t.Run("collision with a frontend rpc port is rejected", func(t *testing.T) {
		c := defaulted(&v1beta1.PProfSpec{Enabled: true, Port: ptr.To(int32(7233))})
		_, errs := c.Spec.ValidatePProf()
		if assert.Len(t, errs, 1) {
			assert.Equal(t, "spec.pprof.port", errs[0].Field)
			assert.Contains(t, errs[0].Detail, "frontend service rpc")
		}
	})

	t.Run("collision with a membership port is rejected", func(t *testing.T) {
		c := defaulted(&v1beta1.PProfSpec{Enabled: true, Port: ptr.To(int32(6934))})
		_, errs := c.Spec.ValidatePProf()
		if assert.Len(t, errs, 1) {
			assert.Contains(t, errs[0].Detail, "history service membership")
		}
	})

	t.Run("collision with the frontend http port is rejected", func(t *testing.T) {
		c := defaulted(&v1beta1.PProfSpec{Enabled: true, Port: ptr.To(int32(7243))})
		_, errs := c.Spec.ValidatePProf()
		if assert.Len(t, errs, 1) {
			assert.Contains(t, errs[0].Detail, "frontend service http")
		}
	})

	t.Run("services without an http port don't collide on the zero default", func(t *testing.T) {
		// history/matching/worker and the internal frontend all default httpPort to 0;
		// they must not be reported as colliding with a real pprof port.
		c := defaulted(&v1beta1.PProfSpec{Enabled: true})
		_, errs := c.Spec.ValidatePProf()
		assert.Empty(t, errs)
	})

	t.Run("collision with the metrics port is rejected", func(t *testing.T) {
		c := cluster(v1beta1.TemporalClusterSpec{
			NumHistoryShards: 1,
			PProf:            &v1beta1.PProfSpec{Enabled: true, Port: ptr.To(int32(9090))},
			Metrics: &v1beta1.MetricsSpec{
				Enabled:    true,
				Prometheus: &v1beta1.PrometheusSpec{ListenPort: ptr.To(int32(9090))},
			},
		})
		c.Default()

		_, errs := c.Spec.ValidatePProf()
		if assert.Len(t, errs, 1) {
			assert.Contains(t, errs[0].Detail, "metrics")
		}
	})
}
