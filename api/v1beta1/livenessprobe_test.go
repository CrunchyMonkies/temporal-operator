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
	corev1 "k8s.io/api/core/v1"
)

func TestLivenessProbeSpecResolve(t *testing.T) {
	defaultProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{},
		},
	}
	customProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{"true"}},
		},
	}

	tests := map[string]struct {
		spec     *v1beta1.LivenessProbeSpec
		expected *corev1.Probe
	}{
		"unset spec keeps the default": {
			spec:     nil,
			expected: defaultProbe,
		},
		"empty spec keeps the default": {
			spec:     &v1beta1.LivenessProbeSpec{},
			expected: defaultProbe,
		},
		"disabled removes the probe": {
			spec:     &v1beta1.LivenessProbeSpec{Disabled: true},
			expected: nil,
		},
		"custom probe replaces the default": {
			spec:     &v1beta1.LivenessProbeSpec{Probe: customProbe},
			expected: customProbe,
		},
		"disabled wins over a custom probe": {
			spec:     &v1beta1.LivenessProbeSpec{Disabled: true, Probe: customProbe},
			expected: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.spec.Resolve(defaultProbe))
		})
	}
}

// TestLivenessProbeSpecResolveNilSafe asserts the accessor can be called on a nil
// spec, which is what the deployment builders do when the field is unset.
func TestLivenessProbeSpecResolveNilSafe(t *testing.T) {
	var spec *v1beta1.LivenessProbeSpec

	defaultProbe := &corev1.Probe{}
	assert.Equal(t, defaultProbe, spec.Resolve(defaultProbe))
	assert.Nil(t, spec.Resolve(nil))
}
