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

package bootstrap_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexandrevilain/temporal-operator/internal/bootstrap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const webhookConfiguration = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: temporal-operator-validating-webhook-configuration
webhooks:
- name: vtemporalc.kb.io
  admissionReviewVersions:
  - v1
  sideEffects: None
  clientConfig:
    url: https://operator.example.com:9443/validate
`

func TestDecode(t *testing.T) {
	tests := map[string]struct {
		manifest      string
		expectedKinds []string
		expectedError string
	}{
		"single document": {
			manifest:      webhookConfiguration,
			expectedKinds: []string{"ValidatingWebhookConfiguration"},
		},
		"multiple documents with leading and trailing separators": {
			manifest: "---\n" + webhookConfiguration + "---\n" + strings.Replace(
				webhookConfiguration, "ValidatingWebhookConfiguration", "MutatingWebhookConfiguration", 1,
			) + "---\n",
			expectedKinds: []string{"ValidatingWebhookConfiguration", "MutatingWebhookConfiguration"},
		},
		"empty manifest": {
			manifest:      "",
			expectedKinds: []string{},
		},
		"document missing kind": {
			manifest:      "apiVersion: v1\nmetadata:\n  name: nope\n",
			expectedError: "'Kind' is missing",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			objects, err := bootstrap.Decode(strings.NewReader(test.manifest))
			if test.expectedError != "" {
				require.ErrorContains(t, err, test.expectedError)
				return
			}

			require.NoError(t, err)

			kinds := []string{}
			for _, obj := range objects {
				kinds = append(kinds, obj.GetKind())
			}
			assert.Equal(t, test.expectedKinds, kinds)
		})
	}
}

func TestLoadIgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "webhook.yaml"), []byte(webhookConfiguration), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a manifest"), 0o600))
	// Mounted ConfigMaps hold their content behind a hidden directory. Walking into it would
	// apply every manifest twice.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "..data"), 0o700))

	objects, err := bootstrap.Load(dir)
	require.NoError(t, err)
	require.Len(t, objects, 1)
	assert.Equal(t, "ValidatingWebhookConfiguration", objects[0].GetKind())
}

func TestInjectCABundle(t *testing.T) {
	caBundle := []byte("-----BEGIN CERTIFICATE-----\nnot-a-real-cert\n-----END CERTIFICATE-----")
	encoded := base64.StdEncoding.EncodeToString(caBundle)

	t.Run("sets the bundle on webhooks without one", func(t *testing.T) {
		objects, err := bootstrap.Decode(strings.NewReader(webhookConfiguration))
		require.NoError(t, err)

		require.NoError(t, bootstrap.InjectCABundle(objects[0], caBundle))
		assert.Equal(t, encoded, caBundleOf(t, objects[0]))
	})

	t.Run("leaves an existing bundle alone", func(t *testing.T) {
		objects, err := bootstrap.Decode(strings.NewReader(
			strings.Replace(webhookConfiguration, "    url:", "    caBundle: YWxyZWFkeS1zZXQ=\n    url:", 1),
		))
		require.NoError(t, err)

		require.NoError(t, bootstrap.InjectCABundle(objects[0], caBundle))
		assert.Equal(t, "YWxyZWFkeS1zZXQ=", caBundleOf(t, objects[0]))
	})

	t.Run("ignores an empty bundle", func(t *testing.T) {
		objects, err := bootstrap.Decode(strings.NewReader(webhookConfiguration))
		require.NoError(t, err)

		require.NoError(t, bootstrap.InjectCABundle(objects[0], nil))
		assert.Empty(t, caBundleOf(t, objects[0]))
	})

	t.Run("ignores other kinds", func(t *testing.T) {
		crd := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata":   map[string]interface{}{"name": "temporalclusters.temporal.io"},
		}}

		require.NoError(t, bootstrap.InjectCABundle(crd, caBundle))
		_, found, err := unstructured.NestedSlice(crd.Object, "webhooks")
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func caBundleOf(t *testing.T, obj *unstructured.Unstructured) string {
	t.Helper()

	webhooks, found, err := unstructured.NestedSlice(obj.Object, "webhooks")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, webhooks, 1)

	webhook, ok := webhooks[0].(map[string]interface{})
	require.True(t, ok)

	caBundle, _, err := unstructured.NestedString(webhook, "clientConfig", "caBundle")
	require.NoError(t, err)

	return caBundle
}
