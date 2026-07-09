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

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestSchemeRegistersThunderApplication is a compile-anchor test: it asserts
// that ThunderApplication is registered with the scheme and round-trips
// through runtime.Object deepcopy, which only compiles once the hand-written
// deepcopy.go satisfies the runtime.Object interface. It also guards
// field-copy completeness — see the doc comment in deepcopy.go.
func TestSchemeRegistersThunderApplication(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	original := &ThunderApplication{
		TypeMeta:   metav1.TypeMeta{Kind: "ThunderApplication", APIVersion: "aep.wso2.com/v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "default"},
		Spec: ThunderApplicationSpec{
			DisplayName:  "My App",
			Scopes:       "openid profile email",
			RedirectURIs: "https://example.com/callback",
		},
		Status: ThunderApplicationStatus{
			Ready:    true,
			ClientID: "client-123",
		},
	}

	if !scheme.Recognizes(original.GroupVersionKind()) {
		t.Fatalf("scheme does not recognize %s", original.GroupVersionKind())
	}

	// DeepCopyObject exercises the generated deepcopy and satisfies
	// runtime.Object — the compile-anchor for this test.
	var obj runtime.Object = original
	copied := obj.DeepCopyObject().(*ThunderApplication)

	if copied == original {
		t.Fatal("DeepCopyObject returned the same pointer, expected a copy")
	}
	if copied.Spec != original.Spec {
		t.Fatalf("Spec mismatch after deepcopy: got %+v, want %+v", copied.Spec, original.Spec)
	}
	if copied.Status != original.Status {
		t.Fatalf("Status mismatch after deepcopy: got %+v, want %+v", copied.Status, original.Status)
	}
	if copied.Name != original.Name || copied.Namespace != original.Namespace {
		t.Fatalf("ObjectMeta mismatch after deepcopy: got %+v, want %+v", copied.ObjectMeta, original.ObjectMeta)
	}

	// ThunderApplicationList must also satisfy runtime.Object via its own
	// deepcopy.
	list := &ThunderApplicationList{Items: []ThunderApplication{*original}}
	var listObj runtime.Object = list
	copiedList := listObj.DeepCopyObject().(*ThunderApplicationList)
	if copiedList == list {
		t.Fatal("DeepCopyObject returned the same pointer, expected a copy")
	}
	if len(copiedList.Items) != 1 {
		t.Fatalf("expected 1 item in copied list, got %d", len(copiedList.Items))
	}
	if copiedList.Items[0].Spec != list.Items[0].Spec {
		t.Fatalf("Items[0].Spec mismatch after deepcopy: got %+v, want %+v", copiedList.Items[0].Spec, list.Items[0].Spec)
	}

	// Mutating the copy must not affect the original — proves the Items
	// slice (and each element) was actually deep-copied, not aliased.
	copiedList.Items[0].Spec.DisplayName = "mutated"
	if list.Items[0].Spec.DisplayName == "mutated" {
		t.Fatal("mutating copiedList.Items affected the original list — Items slice was not deep-copied")
	}
}
