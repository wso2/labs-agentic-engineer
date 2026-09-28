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

package envidp

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestFakeClientset_SupportsApply is a smoke test guarding an assumption the
// rest of this package's tests depend on: that k8s.io/client-go's fake
// Clientset implements server-side apply for ConfigMaps/Secrets via its
// field-managed object tracker, so applyConfigMap/applySecret can be
// exercised against it directly rather than requiring a real API server or a
// hand-rolled Create-or-Update fallback in this package's own code.
//
// fake.NewClientset (not NewSimpleClientset) is required for this: only its
// FieldManagedObjectTracker understands the apply patch type at all —
// NewSimpleClientset's plain ObjectTracker returns a bare NotFound for one,
// which is exactly the failure mode this test would otherwise mask.
func TestFakeClientset_SupportsApply(t *testing.T) {
	client := fake.NewClientset()
	ctx := context.Background()

	if _, err := client.CoreV1().ConfigMaps("ns").Apply(ctx,
		applyConfigMap("ns", "cm1", map[string]string{"k": "v"}, map[string]string{"l": "1"}),
		metav1.ApplyOptions{FieldManager: "test", Force: true},
	); err != nil {
		t.Fatalf("apply ConfigMap: %v", err)
	}
	got, err := client.CoreV1().ConfigMaps("ns").Get(ctx, "cm1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ConfigMap after apply: %v", err)
	}
	if got.Data["k"] != "v" {
		t.Errorf("ConfigMap data = %v, want k=v", got.Data)
	}
	if got.Labels["l"] != "1" {
		t.Errorf("ConfigMap labels = %v, want l=1", got.Labels)
	}

	// Re-apply with changed data converges rather than erroring.
	if _, err := client.CoreV1().ConfigMaps("ns").Apply(ctx,
		applyConfigMap("ns", "cm1", map[string]string{"k": "v2"}, map[string]string{"l": "1"}),
		metav1.ApplyOptions{FieldManager: "test", Force: true},
	); err != nil {
		t.Fatalf("re-apply ConfigMap: %v", err)
	}
	got, err = client.CoreV1().ConfigMaps("ns").Get(ctx, "cm1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ConfigMap after re-apply: %v", err)
	}
	if got.Data["k"] != "v2" {
		t.Errorf("ConfigMap data after re-apply = %v, want k=v2", got.Data)
	}
}
