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

package kubernetes_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/pkg/kubernetes"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func metaWith(annotations, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Annotations: annotations, Labels: labels}
}

func TestSetServicePorts(t *testing.T) {
	tests := map[string]struct {
		service  *corev1.Service
		desired  []corev1.ServicePort
		expected []corev1.ServicePort
	}{
		"sets ports on a new service": {
			service: &corev1.Service{},
			desired: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233},
			},
			expected: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233},
			},
		},
		"keeps node ports allocated by the api server": {
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
						{Name: "http", Port: 7243, NodePort: 30124},
					},
				},
			},
			desired: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233},
				{Name: "http", Port: 7243},
			},
			expected: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
				{Name: "http", Port: 7243, NodePort: 30124},
			},
		},
		"user-provided node port takes precedence over the allocated one": {
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
					},
				},
			},
			desired: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233, NodePort: 32000},
			},
			expected: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233, NodePort: 32000},
			},
		},
		"removed ports are dropped": {
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
						{Name: "http", Port: 7243, NodePort: 30124},
					},
				},
			},
			desired: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233},
			},
			expected: []corev1.ServicePort{
				{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(tt *testing.T) {
			kubernetes.SetServicePorts(test.service, test.desired)
			assert.Equal(tt, test.expected, test.service.Spec.Ports)
		})
	}
}

func TestApplyServiceResourceSpec(t *testing.T) {
	newService := func() *corev1.Service {
		return &corev1.Service{
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "grpc-rpc", Port: 7233},
					{Name: "http", Port: 7243},
				},
			},
		}
	}

	tests := map[string]struct {
		service  *corev1.Service
		spec     *v1beta1.ServiceResourceSpec
		expected *corev1.Service
	}{
		"defaults to ClusterIP when the spec is not set": {
			service: newService(),
			spec:    nil,
			expected: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233},
						{Name: "http", Port: 7243},
					},
				},
			},
		},
		"sets the type, the annotations and the labels": {
			service: newService(),
			spec: &v1beta1.ServiceResourceSpec{
				ObjectMetaOverride: v1beta1.ObjectMetaOverride{
					Annotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-internal": "true"},
					Labels:      map[string]string{"my": "label"},
				},
				Type: ptr.To(corev1.ServiceTypeLoadBalancer),
			},
			expected: &corev1.Service{
				ObjectMeta: metaWith(
					map[string]string{"service.beta.kubernetes.io/aws-load-balancer-internal": "true"},
					map[string]string{"my": "label"},
				),
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233},
						{Name: "http", Port: 7243},
					},
				},
			},
		},
		"sets the node port on the requested port only": {
			service: newService(),
			spec: &v1beta1.ServiceResourceSpec{
				Type:     ptr.To(corev1.ServiceTypeNodePort),
				NodePort: ptr.To(int32(32000)),
			},
			expected: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeNodePort,
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 32000},
						{Name: "http", Port: 7243},
					},
				},
			},
		},
		"doesn't revert fields set by the user": {
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:                     corev1.ServiceTypeLoadBalancer,
					LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
					ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyTypeLocal,
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
					},
				},
			},
			spec: &v1beta1.ServiceResourceSpec{
				ObjectMetaOverride: v1beta1.ObjectMetaOverride{
					Annotations: map[string]string{"a": "b"},
				},
			},
			expected: &corev1.Service{
				ObjectMeta: metaWith(map[string]string{"a": "b"}, nil),
				Spec: corev1.ServiceSpec{
					Type:                     corev1.ServiceTypeLoadBalancer,
					LoadBalancerSourceRanges: []string{"10.0.0.0/8"},
					ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyTypeLocal,
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
					},
				},
			},
		},
		"sets back the type to ClusterIP when explicitly requested": {
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
					},
				},
			},
			spec: &v1beta1.ServiceResourceSpec{
				Type: ptr.To(corev1.ServiceTypeClusterIP),
			},
			expected: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{Name: "grpc-rpc", Port: 7233, NodePort: 30123},
					},
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(tt *testing.T) {
			kubernetes.ApplyServiceResourceSpec(test.service, test.spec, "grpc-rpc")
			assert.Equal(tt, test.expected, test.service)
		})
	}
}
