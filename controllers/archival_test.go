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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
)

// createArchivalCluster creates a cluster archiving to s3 with the given archiver, and reads it
// back from the api server. Anything the CRD schema doesn't know about is pruned on the way in, so
// what comes back is what the operator will actually see.
func createArchivalCluster(ctx context.Context, name string, s3 *v1beta1.S3Archiver) *v1beta1.TemporalCluster {
	cluster := &v1beta1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1beta1.TemporalClusterSpec{
			NumHistoryShards: 1,
			Persistence: v1beta1.TemporalPersistenceSpec{
				DefaultStore:    &v1beta1.DatastoreSpec{},
				VisibilityStore: &v1beta1.DatastoreSpec{},
			},
			Archival: &v1beta1.ClusterArchivalSpec{
				Enabled:  true,
				Provider: &v1beta1.ArchivalProvider{S3: s3},
				History: &v1beta1.ArchivalSpec{
					Enabled: true,
					Path:    "my-bucket",
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

	stored := &v1beta1.TemporalCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, stored)).To(Succeed())

	return stored
}

var _ = Describe("S3 archival credentials", func() {
	ctx := context.Background()

	It("keeps the default credential chain opt-in", func() {
		stored := createArchivalCluster(ctx, "archival-default-credentials", &v1beta1.S3Archiver{
			Region:                "eu-west-1",
			UseDefaultCredentials: true,
		})

		Expect(stored.Spec.Archival.Provider.S3.UseDefaultCredentials).To(BeTrue())
		Expect(stored.Spec.Archival.Provider.S3.RoleName).To(BeNil())
		Expect(stored.Spec.Archival.Provider.S3.Credentials).To(BeNil())
		Expect(stored.Spec.Archival.Provider.S3.UsesIRSA()).To(BeFalse())
	})

	It("leaves an existing IRSA cluster alone", func() {
		roleName := "arn:aws:iam::123456789012:role/temporal-archival"

		stored := createArchivalCluster(ctx, "archival-irsa", &v1beta1.S3Archiver{
			Region:   "eu-west-1",
			RoleName: ptr.To(roleName),
		})

		Expect(stored.Spec.Archival.Provider.S3.RoleName).To(HaveValue(Equal(roleName)))
		Expect(stored.Spec.Archival.Provider.S3.UseDefaultCredentials).To(BeFalse())
		Expect(stored.Spec.Archival.Provider.S3.UsesIRSA()).To(BeTrue())
	})
})
