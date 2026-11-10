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

package kubernetes

import (
	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/metadata"
	corev1 "k8s.io/api/core/v1"
)

// SetServicePorts sets the desired ports on the provided service, carrying over the node ports
// already allocated by the api server for ports of the same name.
// Without this, each reconciliation would send back a 0 node port for services of type NodePort
// or LoadBalancer, making the api server allocate a new node port on every reconciliation.
func SetServicePorts(service *corev1.Service, desired []corev1.ServicePort) {
	allocated := map[string]int32{}
	for _, port := range service.Spec.Ports {
		if port.NodePort != 0 {
			allocated[port.Name] = port.NodePort
		}
	}

	for i, port := range desired {
		if port.NodePort != 0 {
			continue
		}
		if nodePort, ok := allocated[port.Name]; ok {
			desired[i].NodePort = nodePort
		}
	}

	service.Spec.Ports = desired
}

// ApplyServiceResourceSpec applies the user-provided service customizations to the provided service.
// It's a set-only merge: fields which aren't set in the spec are left untouched on the service, so
// that fields the operator doesn't manage (such as spec.loadBalancerSourceRanges) are not reverted
// at each reconciliation. The only exception is the service type, which is set to ClusterIP when the
// service doesn't have any type yet, ie. when it's being created.
// nodePortForPort is the name of the service port the spec's node port applies to.
func ApplyServiceResourceSpec(service *corev1.Service, spec *v1beta1.ServiceResourceSpec, nodePortForPort string) {
	if spec == nil {
		setDefaultServiceType(service)
		return
	}

	if len(spec.Labels) > 0 {
		service.Labels = metadata.Merge(service.Labels, spec.Labels)
	}

	if len(spec.Annotations) > 0 {
		service.Annotations = metadata.Merge(service.Annotations, spec.Annotations)
	}

	if spec.Type != nil {
		service.Spec.Type = *spec.Type
	} else {
		setDefaultServiceType(service)
	}

	if spec.NodePort != nil {
		for i, port := range service.Spec.Ports {
			if port.Name == nodePortForPort {
				service.Spec.Ports[i].NodePort = *spec.NodePort
			}
		}
	}
}

func setDefaultServiceType(service *corev1.Service) {
	if service.Spec.Type == "" {
		service.Spec.Type = corev1.ServiceTypeClusterIP
	}
}
