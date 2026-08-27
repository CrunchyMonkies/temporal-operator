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

package temporal

import (
	"context"
	"errors"
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testNamespace = "test-ns"

// mockOperatorServiceClient is a mock for testing.
type mockOperatorServiceClient struct {
	listResponse *operatorservice.ListSearchAttributesResponse
	listError    error
	addError     error
	removeError  error

	addCalled     bool
	removeCalled  bool
	addRequest    *operatorservice.AddSearchAttributesRequest
	removeRequest *operatorservice.RemoveSearchAttributesRequest
}

func (m *mockOperatorServiceClient) ListSearchAttributes(_ context.Context, _ *operatorservice.ListSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error) {
	return m.listResponse, m.listError
}

func (m *mockOperatorServiceClient) AddSearchAttributes(_ context.Context, req *operatorservice.AddSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error) {
	m.addCalled = true
	m.addRequest = req
	return &operatorservice.AddSearchAttributesResponse{}, m.addError
}

func (m *mockOperatorServiceClient) RemoveSearchAttributes(_ context.Context, req *operatorservice.RemoveSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.RemoveSearchAttributesResponse, error) {
	m.removeCalled = true
	m.removeRequest = req
	return &operatorservice.RemoveSearchAttributesResponse{}, m.removeError
}

func newNamespace(attrs map[string]string, allowDeletion bool) *v1beta1.TemporalNamespace {
	return &v1beta1.TemporalNamespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
		Spec: v1beta1.TemporalNamespaceSpec{
			CustomSearchAttributes:       attrs,
			AllowSearchAttributeDeletion: allowDeletion,
		},
	}
}

func TestReconcileSearchAttributes(t *testing.T) {
	ctx := context.Background()

	t.Run("add new attributes", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enums.IndexedValueType{},
			},
		}
		ns := newNamespace(map[string]string{
			"CustomerId": "Keyword",
			"OrderTotal": "Double",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.True(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
		assert.Equal(t, map[string]enums.IndexedValueType{
			"CustomerId": enums.INDEXED_VALUE_TYPE_KEYWORD,
			"OrderTotal": enums.INDEXED_VALUE_TYPE_DOUBLE,
		}, mock.addRequest.SearchAttributes)
		assert.Equal(t, testNamespace, mock.addRequest.Namespace)
	})

	t.Run("remove stale with flag on", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enums.IndexedValueType{
					"OldAttr": enums.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace(map[string]string{}, true)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.False(t, mock.addCalled)
		assert.True(t, mock.removeCalled)
		assert.Equal(t, []string{"OldAttr"}, mock.removeRequest.SearchAttributes)
	})

	t.Run("no removal with flag off", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enums.IndexedValueType{
					"OldAttr": enums.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace(map[string]string{}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.False(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
	})

	t.Run("no changes when spec matches server", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enums.IndexedValueType{
					"CustomerId": enums.INDEXED_VALUE_TYPE_KEYWORD,
				},
			},
		}
		ns := newNamespace(map[string]string{
			"CustomerId": "Keyword",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.False(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
	})

	t.Run("mixed add and remove", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enums.IndexedValueType{
					"Existing": enums.INDEXED_VALUE_TYPE_KEYWORD,
					"OldAttr":  enums.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace(map[string]string{
			"Existing": "Keyword",
			"NewAttr":  "Bool",
		}, true)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.True(t, mock.addCalled)
		assert.True(t, mock.removeCalled)
		assert.Equal(t, map[string]enums.IndexedValueType{
			"NewAttr": enums.INDEXED_VALUE_TYPE_BOOL,
		}, mock.addRequest.SearchAttributes)
		assert.Equal(t, []string{"OldAttr"}, mock.removeRequest.SearchAttributes)
	})

	t.Run("type mismatch returns error", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enums.IndexedValueType{
					"CustomerId": enums.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace(map[string]string{
			"CustomerId": "Keyword",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not allow type changes")
		// The spec is what is wrong, so the caller can stop retrying rather than back off.
		assert.ErrorIs(t, err, ErrInvalidSearchAttributes)
		assert.False(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
	})

	t.Run("invalid type string returns error", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enums.IndexedValueType{},
			},
		}
		ns := newNamespace(map[string]string{
			"CustomerId": "InvalidType",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid search attribute type")
		assert.ErrorIs(t, err, ErrInvalidSearchAttributes)
		assert.False(t, mock.addCalled)
	})

	// A server that cannot be reached is the opposite case: a retry is exactly what it needs.
	t.Run("server failure is not marked as a spec problem", func(t *testing.T) {
		mock := &mockOperatorServiceClient{listError: errors.New("unavailable")}
		ns := newNamespace(map[string]string{"CustomerId": "Keyword"}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrInvalidSearchAttributes)
	})
}
