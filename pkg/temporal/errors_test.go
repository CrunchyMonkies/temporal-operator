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

package temporal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/alexandrevilain/temporal-operator/pkg/temporal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// replay pushes errs through the recorder's interceptor, one per attempt, as the SDK's retries
// would, and returns the error the caller ends up with.
func replay(t *testing.T, recorder *temporal.CallRecorder, errs ...error) error {
	t.Helper()

	interceptor := recorder.UnaryClientInterceptor()

	var last error
	for _, err := range errs {
		invoker := func(_ context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			return err
		}
		last = interceptor(context.Background(), "/temporal.api.workflowservice.v1.WorkflowService/RegisterNamespace", nil, nil, nil, invoker)
	}

	return last
}

func TestCallRecorderExplain(t *testing.T) {
	// What the frontend returns when the archiver can't assume its role: not retryable in itself,
	// but the SDK retries the whole call until the deadline.
	accessDenied := status.Error(codes.Internal, "AccessDenied: User is not authorized to perform sts:AssumeRole")
	deadline := status.Error(codes.DeadlineExceeded, "context deadline exceeded")

	tests := map[string]struct {
		// attempts are the errors the server returns, in order.
		attempts []error
		// callErr is what the sdk hands back to the caller once it gives up.
		callErr error
		// expected is the message Explain has to produce.
		expected string
	}{
		"a deadline is explained with the last server error": {
			attempts: []error{accessDenied, accessDenied, deadline},
			callErr:  context.DeadlineExceeded,
			expected: "context deadline exceeded, last error returned by the server: rpc error: code = Internal desc = AccessDenied: User is not authorized to perform sts:AssumeRole",
		},
		"a service error deadline is explained too": {
			attempts: []error{accessDenied},
			callErr:  &serviceerror.DeadlineExceeded{Message: "context deadline exceeded"},
			expected: "context deadline exceeded, last error returned by the server: rpc error: code = Internal desc = AccessDenied: User is not authorized to perform sts:AssumeRole",
		},
		"a deadline with nothing recorded is left alone": {
			attempts: []error{deadline},
			callErr:  context.DeadlineExceeded,
			expected: "context deadline exceeded",
		},
		"an error which already says what went wrong is left alone": {
			attempts: []error{accessDenied},
			callErr:  errors.New("namespace already exists"),
			expected: "namespace already exists",
		},
		"a recovered attempt doesn't explain a later deadline": {
			attempts: []error{accessDenied, nil},
			callErr:  context.DeadlineExceeded,
			expected: "context deadline exceeded",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			recorder := temporal.NewCallRecorder()
			replay(t, recorder, test.attempts...)

			assert.Equal(t, test.expected, recorder.Explain(test.callErr).Error())
		})
	}
}

func TestCallRecorderExplainNilError(t *testing.T) {
	recorder := temporal.NewCallRecorder()
	replay(t, recorder, status.Error(codes.Internal, "AccessDenied"))

	assert.NoError(t, recorder.Explain(nil))
}

// TestCallRecorderExplainKeepsBothErrors makes sure the callers matching on the error they got —
// the namespace reconciler looks for NamespaceAlreadyExists — still find it after Explain wrapped
// the server error into it.
func TestCallRecorderExplainKeepsBothErrors(t *testing.T) {
	alreadyExists := serviceerror.NewNamespaceAlreadyExists("namespace already exists")

	recorder := temporal.NewCallRecorder()
	replay(t, recorder, alreadyExists)

	err := recorder.Explain(context.DeadlineExceeded)
	require.Error(t, err)

	assert.ErrorIs(t, err, context.DeadlineExceeded)

	var namespaceAlreadyExists *serviceerror.NamespaceAlreadyExists
	assert.ErrorAs(t, err, &namespaceAlreadyExists)
}

// TestCallRecorderInterceptorIsTransparent makes sure recording doesn't change what the client
// sees for an attempt.
func TestCallRecorderInterceptorIsTransparent(t *testing.T) {
	recorder := temporal.NewCallRecorder()

	assert.NoError(t, replay(t, recorder, nil))

	accessDenied := status.Error(codes.Internal, "AccessDenied")
	assert.Equal(t, accessDenied, replay(t, recorder, accessDenied))
}
