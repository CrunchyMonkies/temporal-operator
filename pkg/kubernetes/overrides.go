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
	"encoding/json"
	"fmt"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/metadata"
	jsonpatch "github.com/evanphx/json-patch/v5"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// PatchPodSpecWithOverride patches the provided pod spec with the provided pod spec override.
func PatchPodSpecWithOverride(spec, override *corev1.PodSpec) (*corev1.PodSpec, error) {
	if override == nil {
		return nil, nil //nolint:nilnil
	}

	orginalSpec, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("can't marshal pod spec: %w", err)
	}

	overrideSpec, err := json.Marshal(override)
	if err != nil {
		return nil, fmt.Errorf("can't marshal pod spec override: %w", err)
	}

	patchedJSON, err := strategicpatch.StrategicMergePatch(orginalSpec, overrideSpec, corev1.PodSpec{})
	if err != nil {
		return nil, fmt.Errorf("can't patch pod spec: %w", err)
	}

	patchedSpec := &corev1.PodSpec{}
	err = json.Unmarshal(patchedJSON, patchedSpec)
	if err != nil {
		return nil, fmt.Errorf("can't unmarshal patched pod spec: %w", err)
	}

	if errs := validatePatchedPodSpec(patchedSpec, field.NewPath("spec")); len(errs) > 0 {
		return nil, fmt.Errorf("invalid patched pod spec: %w", errs.ToAggregate())
	}

	return patchedSpec, nil
}

// ApplyPodTemplateOverrides applies the provided PodTemplateOverride to the provided PodTemplateSpec.
func ApplyPodTemplateSpecOverrides(podTemplate *corev1.PodTemplateSpec, override *v1beta1.PodTemplateSpecOverride) error {
	if override == nil {
		return nil
	}

	if override.ObjectMetaOverride != nil {
		if len(override.Labels) > 0 {
			podTemplate.Labels = metadata.Merge(podTemplate.Labels, override.Labels)
		}
		if len(override.Annotations) > 0 {
			podTemplate.Annotations = metadata.Merge(podTemplate.Annotations, override.Annotations)
		}
	}

	if override.Spec != nil {
		original, err := json.Marshal(podTemplate.Spec)
		if err != nil {
			return fmt.Errorf("can't marshal pod template spec: %w", err)
		}
		patched, err := strategicpatch.StrategicMergePatch(original, override.Spec.Raw, podTemplate.Spec)
		if err != nil {
			return fmt.Errorf("can't patch pod template spec: %w", err)
		}
		// Decode into a fresh PodSpec: the strategic merge reorders merge-key lists
		// (env, volumes, ...) so unmarshalling in place would decode a merged element
		// on top of an unrelated existing one and keep its stale fields, producing
		// invalid results such as an EnvVar holding both value and valueFrom.
		patchedSpec := corev1.PodSpec{}
		if err := json.Unmarshal(patched, &patchedSpec); err != nil {
			return fmt.Errorf("can't unmarshal patched pod template spec: %w", err)
		}

		if errs := validatePatchedPodSpec(&patchedSpec, field.NewPath("spec")); len(errs) > 0 {
			return fmt.Errorf("invalid patched pod template spec: %w", errs.ToAggregate())
		}

		podTemplate.Spec = patchedSpec

		return nil
	}
	return nil
}

// ApplyDeploymentOverrides applies the provided DeploymentOverride to the provided Deployment.
func ApplyDeploymentOverrides(deployment *appsv1.Deployment, override *v1beta1.DeploymentOverride) error {
	if override == nil {
		return nil
	}

	if override.ObjectMetaOverride != nil {
		if len(override.Labels) > 0 {
			deployment.Labels = metadata.Merge(deployment.Labels, override.Labels)
		}

		if len(override.Annotations) > 0 {
			deployment.Annotations = metadata.Merge(deployment.Annotations, override.Annotations)
		}
	}

	if override.Spec != nil {
		err := ApplyPodTemplateSpecOverrides(&deployment.Spec.Template, override.Spec.Template)
		if err != nil {
			return err
		}
	}

	if override.JSONPatch != nil {
		patch, err := jsonpatch.DecodePatch(override.JSONPatch.Raw)
		if err != nil {
			return fmt.Errorf("can't decode json patch: %w", err)
		}

		original, err := json.Marshal(deployment)
		if err != nil {
			return fmt.Errorf("can't marshal deployment spec: %w", err)
		}

		patched, err := patch.Apply(original)
		if err != nil {
			return fmt.Errorf("can't apply json patch: %w", err)
		}
		// Decode into a fresh Deployment for the same reason as above: a json patch
		// can reorder or remove list elements, and unmarshalling in place would leave
		// stale fields behind on the elements it decodes over.
		patchedDeployment := appsv1.Deployment{}
		if err := json.Unmarshal(patched, &patchedDeployment); err != nil {
			return fmt.Errorf("can't unmarshal patched deployment: %w", err)
		}

		errs := validatePatchedPodSpec(&patchedDeployment.Spec.Template.Spec, field.NewPath("spec", "template", "spec"))
		if len(errs) > 0 {
			return fmt.Errorf("invalid patched deployment: %w", errs.ToAggregate())
		}

		*deployment = patchedDeployment

		return nil
	}

	return nil
}

// ApplyServiceOverrides applies the provided ServiceOverride to the provided Service.
func ApplyServiceOverrides(service *corev1.Service, override *v1beta1.ObjectMetaOverride) error {
	if override == nil {
		return nil
	}

	if len(override.Labels) > 0 {
		service.Labels = metadata.Merge(service.Labels, override.Labels)
	}

	if len(override.Annotations) > 0 {
		service.Annotations = metadata.Merge(service.Annotations, override.Annotations)
	}
	return nil
}
