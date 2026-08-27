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

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ErrInvalidSearchAttributes marks a failure the namespace spec causes rather than the server, so
// callers can tell it apart from a transient RPC failure and stop retrying it.
var ErrInvalidSearchAttributes = errors.New("invalid search attributes")

// searchAttributeTypes maps user-facing type names to Temporal IndexedValueType.
var searchAttributeTypes = map[string]enums.IndexedValueType{
	"Text":        enums.INDEXED_VALUE_TYPE_TEXT,
	"Keyword":     enums.INDEXED_VALUE_TYPE_KEYWORD,
	"Int":         enums.INDEXED_VALUE_TYPE_INT,
	"Double":      enums.INDEXED_VALUE_TYPE_DOUBLE,
	"Bool":        enums.INDEXED_VALUE_TYPE_BOOL,
	"DateTime":    enums.INDEXED_VALUE_TYPE_DATETIME,
	"KeywordList": enums.INDEXED_VALUE_TYPE_KEYWORD_LIST,
}

// searchAttributeTypeNames is the reverse mapping from IndexedValueType to string.
var searchAttributeTypeNames = map[enums.IndexedValueType]string{
	enums.INDEXED_VALUE_TYPE_TEXT:         "Text",
	enums.INDEXED_VALUE_TYPE_KEYWORD:      "Keyword",
	enums.INDEXED_VALUE_TYPE_INT:          "Int",
	enums.INDEXED_VALUE_TYPE_DOUBLE:       "Double",
	enums.INDEXED_VALUE_TYPE_BOOL:         "Bool",
	enums.INDEXED_VALUE_TYPE_DATETIME:     "DateTime",
	enums.INDEXED_VALUE_TYPE_KEYWORD_LIST: "KeywordList",
}

// SearchAttributeTypeFromString converts a user-facing type name to its IndexedValueType.
func SearchAttributeTypeFromString(s string) (enums.IndexedValueType, error) {
	t, ok := searchAttributeTypes[s]
	if !ok {
		return enums.INDEXED_VALUE_TYPE_UNSPECIFIED, fmt.Errorf("%w: invalid search attribute type %q: valid types are Text, Keyword, Int, Double, Bool, DateTime, KeywordList", ErrInvalidSearchAttributes, s)
	}
	return t, nil
}

// SearchAttributeTypeToString converts an IndexedValueType to its user-facing name.
func SearchAttributeTypeToString(t enums.IndexedValueType) (string, error) {
	name, ok := searchAttributeTypeNames[t]
	if !ok {
		return "", fmt.Errorf("unknown IndexedValueType: %v", t)
	}
	return name, nil
}

// OperatorServiceClient is an interface for the Temporal OperatorService gRPC methods
// needed by search attribute reconciliation. It is satisfied by the client returned
// from temporalclient.Client.OperatorService().
type OperatorServiceClient interface {
	ListSearchAttributes(ctx context.Context, in *operatorservice.ListSearchAttributesRequest, opts ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error)
	AddSearchAttributes(ctx context.Context, in *operatorservice.AddSearchAttributesRequest, opts ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error)
	RemoveSearchAttributes(ctx context.Context, in *operatorservice.RemoveSearchAttributesRequest, opts ...grpc.CallOption) (*operatorservice.RemoveSearchAttributesResponse, error)
}

// parseDesiredAttributes converts the spec's string type map into typed IndexedValueType map.
func parseDesiredAttributes(spec map[string]string) (map[string]enums.IndexedValueType, error) {
	desired := make(map[string]enums.IndexedValueType, len(spec))
	for name, typeStr := range spec {
		t, err := SearchAttributeTypeFromString(typeStr)
		if err != nil {
			return nil, fmt.Errorf("search attribute %q: %w", name, err)
		}
		desired[name] = t
	}
	return desired, nil
}

// computeAttributesToAdd returns attributes present in desired but not in existing,
// and returns an error if any existing attribute has a type mismatch.
func computeAttributesToAdd(desired, existing map[string]enums.IndexedValueType) (map[string]enums.IndexedValueType, error) {
	toAdd := make(map[string]enums.IndexedValueType)
	for name, desiredType := range desired {
		existingType, exists := existing[name]
		if !exists {
			toAdd[name] = desiredType
			continue
		}
		if existingType != desiredType {
			existingTypeName, _ := SearchAttributeTypeToString(existingType)
			desiredTypeName, _ := SearchAttributeTypeToString(desiredType)
			return nil, fmt.Errorf("%w: search attribute %q has type %s on server but %s in spec; Temporal does not allow type changes", ErrInvalidSearchAttributes, name, existingTypeName, desiredTypeName)
		}
	}
	return toAdd, nil
}

// computeAttributesToRemove returns attribute names present in existing but not in desired.
func computeAttributesToRemove(desired, existing map[string]enums.IndexedValueType) []string {
	var toRemove []string
	for name := range existing {
		if _, inSpec := desired[name]; !inSpec {
			toRemove = append(toRemove, name)
		}
	}
	return toRemove
}

// ReconcileSearchAttributes ensures the custom search attributes on the Temporal server
// match the desired state declared in the TemporalNamespace spec.
func ReconcileSearchAttributes(ctx context.Context, operatorSvc OperatorServiceClient, namespace *v1beta1.TemporalNamespace) error {
	logger := log.FromContext(ctx)
	nsName := namespace.GetName()

	listResp, err := operatorSvc.ListSearchAttributes(ctx, &operatorservice.ListSearchAttributesRequest{
		Namespace: nsName,
	})
	if err != nil {
		return fmt.Errorf("listing search attributes: %w", err)
	}

	existing := listResp.GetCustomAttributes()
	logger.V(1).Info("Listed existing custom search attributes", "namespace", nsName, "count", len(existing))

	desired, err := parseDesiredAttributes(namespace.Spec.CustomSearchAttributes)
	if err != nil {
		return err
	}

	toAdd, err := computeAttributesToAdd(desired, existing)
	if err != nil {
		return err
	}

	var toRemove []string
	if namespace.Spec.AllowSearchAttributeDeletion {
		toRemove = computeAttributesToRemove(desired, existing)
	}

	if len(toAdd) == 0 && len(toRemove) == 0 {
		logger.V(1).Info("Search attributes are up to date", "namespace", nsName)
		return nil
	}

	if len(toAdd) > 0 {
		logger.Info("Adding search attributes", "namespace", nsName, "count", len(toAdd))
		_, err := operatorSvc.AddSearchAttributes(ctx, &operatorservice.AddSearchAttributesRequest{
			SearchAttributes: toAdd,
			Namespace:        nsName,
		})
		if err != nil {
			return fmt.Errorf("adding search attributes: %w", err)
		}
	}

	if len(toRemove) > 0 {
		logger.Info("Removing search attributes", "namespace", nsName, "count", len(toRemove), "attributes", toRemove)
		_, err := operatorSvc.RemoveSearchAttributes(ctx, &operatorservice.RemoveSearchAttributesRequest{
			SearchAttributes: toRemove,
			Namespace:        nsName,
		})
		if err != nil {
			return fmt.Errorf("removing search attributes: %w", err)
		}
	}

	return nil
}
