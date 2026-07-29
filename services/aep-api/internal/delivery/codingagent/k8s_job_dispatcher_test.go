// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package codingagent

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordedK8sOp struct {
	verb      string
	gvk       string
	name      string
	namespace string
}

type recordingK8sClient struct {
	client.Client
	ops []recordedK8sOp
}

func newRecordingK8sClient() *recordingK8sClient {
	return &recordingK8sClient{
		Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).Build(),
	}
}

func (r *recordingK8sClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	r.record("create", obj)
	return r.Client.Create(ctx, obj, opts...)
}

func (r *recordingK8sClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	r.record("patch", obj)
	return r.Client.Patch(ctx, obj, patch, opts...)
}

func (r *recordingK8sClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	r.record("delete", obj)
	return r.Client.Delete(ctx, obj, opts...)
}

func (r *recordingK8sClient) record(verb string, obj client.Object) {
	gvk := obj.GetObjectKind().GroupVersionKind().String()
	switch o := obj.(type) {
	case *corev1.Namespace:
		gvk = "v1/Namespace"
	case *corev1.ServiceAccount:
		gvk = "v1/ServiceAccount"
	case *unstructured.Unstructured:
		gvk = o.GroupVersionKind().String()
	}
	r.ops = append(r.ops, recordedK8sOp{
		verb:      verb,
		gvk:       gvk,
		name:      obj.GetName(),
		namespace: obj.GetNamespace(),
	})
}

func validK8sJobInput() K8sJobInput {
	return K8sJobInput{
		RunName:                "ca-run1-0101010101",
		OrgID:                  "acme",
		OrgUUID:                "d3adbeef-1234-4321-abcd-c0ffee123456",
		ProjectID:              "widgets",
		Component:              "svc",
		ExecutionID:            "exec-1",
		RepoURL:                "https://github.com/acme/widgets",
		Prompt:                 "do work",
		IdentityName:           "bot",
		IdentityEmail:          "bot@example.com",
		IdentityLogin:          "bot",
		Bearer:                 "bearer",
		ClusterSecretStoreName: "default",
		AnthropicSR: SecretRef{
			SecretRefName: "acme-anthropic-secrets",
			KVPath:        "user-app-secrets/wc-acme/acme-anthropic-secrets",
			Property:      "api-key",
		},
		GitHubSR: SecretRef{
			SecretRefName: "acme-github-pat-secrets",
			KVPath:        "user-app-secrets/wc-acme/acme-github-pat-secrets",
			Property:      "token",
		},
	}
}

func TestK8sJobDispatcher_Dispatch_RefsPresent_ExternalSecretsOnly(t *testing.T) {
	rec := newRecordingK8sClient()
	d := NewK8sJobDispatcher(rec, "http://platform", "runner:1", "default")

	if _, err := d.Dispatch(context.Background(), validK8sJobInput()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	var secrets, externalSecrets int
	for _, op := range rec.ops {
		if op.gvk == "v1/Secret" {
			secrets++
		}
		if strings.Contains(op.gvk, "ExternalSecret") {
			externalSecrets++
		}
	}
	if secrets != 0 {
		t.Fatalf("expected no plain Secret writes, got %d ops: %+v", secrets, rec.ops)
	}
	if externalSecrets < 2 {
		t.Fatalf("expected anthropic + github ExternalSecrets, got %d ops: %+v", externalSecrets, rec.ops)
	}
}

func TestK8sJobDispatcher_Dispatch_MissingAnthropicRef_NoSecretCreate(t *testing.T) {
	rec := newRecordingK8sClient()
	d := NewK8sJobDispatcher(rec, "http://platform", "runner:1", "default")
	in := validK8sJobInput()
	in.AnthropicSR = SecretRef{}

	_, err := d.Dispatch(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for missing anthropic ref")
	}
	if !strings.Contains(err.Error(), "anthropic") || !strings.Contains(err.Error(), "sm_api") {
		t.Fatalf("error must name the missing anthropic ref, got: %v", err)
	}
	for _, op := range rec.ops {
		if strings.Contains(op.gvk, "Secret") {
			t.Fatalf("must not create any Secret/ExternalSecret on missing ref, saw %+v", op)
		}
	}
}

func TestK8sJobDispatcher_Dispatch_MissingGitHubRef_NoSecretCreate(t *testing.T) {
	rec := newRecordingK8sClient()
	d := NewK8sJobDispatcher(rec, "http://platform", "runner:1", "default")
	in := validK8sJobInput()
	in.GitHubSR.Property = ""

	_, err := d.Dispatch(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for missing github ref")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Fatalf("error must name the missing github ref, got: %v", err)
	}
	for _, op := range rec.ops {
		if strings.Contains(op.gvk, "Secret") {
			t.Fatalf("must not create any Secret/ExternalSecret on missing ref, saw %+v", op)
		}
	}
}
