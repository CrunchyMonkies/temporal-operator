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

const (
	deletionFinalizer = "deletion.finalizers.temporal.io"
	clusterRefField   = "spec.clusterRef.name"
	namespaceRefField = "spec.namespaceRef.name"

	// forceDeleteAnnotation, set to "true", makes the deletion path drop the deletion finalizer
	// without contacting the temporal server. Whatever the resource stands for on the server is
	// left behind, so this is the last resort for a cluster the operator can no longer reach: it
	// is opt-in per object, and nothing the controller decides on its own.
	forceDeleteAnnotation = "temporal.io/force-delete"
)
