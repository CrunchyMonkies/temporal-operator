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
	"fmt"
	"sync"

	"go.temporal.io/api/serviceerror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CallRecorder keeps the last error the server returned for a call, so a call that ends in a
// deadline can be reported with the failure that actually caused it.
//
// The temporal SDK retries retryable server errors inside a single client call, until the call's
// deadline. What the caller then gets back is "context deadline exceeded", and the reason the
// server kept failing — an S3 archiver rejecting the credentials with WebIdentityErr or
// AccessDenied while registering a namespace, for instance — is never seen anywhere but the
// frontend's own logs. Recording the individual attempts is the only place that error still exists
// on this side.
//
// A recorder is bound to one client through WithCallRecorder, so its calls are the only ones it
// sees. It is safe for concurrent use.
type CallRecorder struct {
	mu   sync.Mutex
	last error
}

// NewCallRecorder returns a recorder ready to be passed to WithCallRecorder.
func NewCallRecorder() *CallRecorder {
	return &CallRecorder{}
}

// UnaryClientInterceptor returns the interceptor recording the outcome of every attempt.
//
// It has to sit below the SDK's retry interceptor to see anything but the final error: the dial
// options of the client are applied after the SDK's own, which puts this one closest to the wire.
func (r *CallRecorder) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		err := invoker(ctx, method, req, reply, cc, opts...)
		r.record(err)
		return err
	}
}

// record keeps err as the explanation to report, unless it explains nothing: a deadline is the
// symptom this recorder exists to look behind, and a success says the client recovered.
func (r *CallRecorder) record(err error) {
	if isDeadlineExceeded(err) {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.last = err
}

// last returns the most recent recorded error, or nil if every attempt succeeded or timed out.
func (r *CallRecorder) lastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.last
}

// Explain returns err with the last error the server returned appended to it, when err is a
// deadline and there is something better to say. Any other error is returned unchanged: it already
// says what went wrong.
//
// Both errors are wrapped, so errors.Is and errors.As still find either of them.
func (r *CallRecorder) Explain(err error) error {
	if err == nil || !isDeadlineExceeded(err) {
		return err
	}

	last := r.lastError()
	if last == nil {
		return err
	}

	return fmt.Errorf("%w, last error returned by the server: %w", err, last)
}

// isDeadlineExceeded reports whether err is the client giving up on a deadline, in any of the
// shapes it reaches the caller in: the context error, a gRPC status from an attempt, or the
// service error the SDK converts that status into.
func isDeadlineExceeded(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var deadlineExceeded *serviceerror.DeadlineExceeded
	if errors.As(err, &deadlineExceeded) {
		return true
	}

	return status.Code(err) == codes.DeadlineExceeded
}
