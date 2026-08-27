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

package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func addFinalizer(ctx context.Context, c client.Client, obj client.Object, finalizer string) error {
	return patchFinalizers(ctx, c, obj, func(patched client.Object) bool {
		return controllerutil.AddFinalizer(patched, finalizer)
	})
}

func removeFinalizer(ctx context.Context, c client.Client, obj client.Object, finalizer string) error {
	// Dropping the last finalizer completes the deletion, so a retried removal finds the object gone.
	return client.IgnoreNotFound(removeFinalizerStrict(ctx, c, obj, finalizer))
}

// removeFinalizerStrict reports a gone object instead of treating it as success, for callers that
// have work left to do after the removal and must not carry on against a deleted object.
func removeFinalizerStrict(ctx context.Context, c client.Client, obj client.Object, finalizer string) error {
	return patchFinalizers(ctx, c, obj, func(patched client.Object) bool {
		return controllerutil.RemoveFinalizer(patched, finalizer)
	})
}

// patchFinalizers writes its own copy of obj, keeping finalizers out of the reconciliation's
// deferred patch: that patch is a JSON merge patch against a snapshot, so it would rewrite
// metadata.finalizers as a whole list and undo what the API server did to foregroundDeletion.
func patchFinalizers(ctx context.Context, c client.Client, obj client.Object, mutate func(client.Object) bool) error {
	patched := obj.DeepCopyObject().(client.Object)
	if !mutate(patched) {
		return nil
	}

	return c.Patch(ctx, patched, client.MergeFromWithOptions(obj, client.MergeFromWithOptimisticLock{}))
}

// confirmLive reports whether obj is still worth acting for: present, and not terminating. Callers
// use it immediately before a change they cannot take back outside the cluster.
//
// It reads through an uncached reader deliberately. A reconciler's own client is backed by an
// informer cache that may not have observed a deletion yet, and the finalizer machinery above is no
// help here either: an object carrying no finalizer leaves patchFinalizers with nothing to write,
// so no request reaches the API server and the deletion goes unnoticed.
func confirmLive(ctx context.Context, r client.Reader, obj client.Object) (bool, error) {
	fresh := obj.DeepCopyObject().(client.Object)
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return fresh.GetDeletionTimestamp().IsZero(), nil
}
