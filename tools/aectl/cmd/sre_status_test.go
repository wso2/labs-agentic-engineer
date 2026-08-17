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

package cmd

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// The present-but-empty case is the one this function exists for: an empty
// string is a legal Secret value, and setup-observability.sh creates
// rca-agent-secret unconditionally even when deployments/.env carries no key. A
// plain existence check reports that install as healthy, which is what sent a
// broken RCA agent all the way to its first analysis before failing.
func TestSecretKeyNonEmpty(t *testing.T) {
	const (
		ns   = "openchoreo-observability-plane"
		name = "rca-agent-secret"
		key  = "RCA_LLM_API_KEY"
	)

	tests := []struct {
		name   string
		secret *corev1.Secret
		want   bool
	}{
		{
			name: "populated key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Data:       map[string][]byte{key: []byte("sk-ant-api03-xxx")},
			},
			want: true,
		},
		{
			name: "present but empty value",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Data:       map[string][]byte{key: []byte("")},
			},
			want: false,
		},
		{
			name: "secret exists, key absent",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Data:       map[string][]byte{"OAUTH_CLIENT_SECRET": []byte("shh")},
			},
			want: false,
		},
		{
			name:   "secret missing entirely",
			secret: nil,
			want:   false,
		},
		{
			name: "secret in a different namespace does not count",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Data:       map[string][]byte{key: []byte("sk-ant-api03-xxx")},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			if tt.secret != nil {
				client = fake.NewSimpleClientset(tt.secret)
			}
			got, err := secretKeyNonEmpty(context.Background(), client, ns, name, key)
			if err != nil {
				t.Fatalf("secretKeyNonEmpty() error = %v, want nil: a Secret that is absent, keyless or empty is a resolved answer, not a failure to look", err)
			}
			if got != tt.want {
				t.Errorf("secretKeyNonEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

// An unreadable Secret — RBAC denial, a partial apiserver outage — is not
// evidence that the key is missing. Folding it into false made `sre status`
// report "NONE — analyses will fail" for an install it never managed to inspect,
// which is the same false-signal class the RCA key check was added to remove.
func TestSecretKeyNonEmptyPropagatesAPIErrors(t *testing.T) {
	client := fake.NewSimpleClientset()
	denied := errors.New("secrets is forbidden: User cannot get resource \"secrets\"")
	client.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, denied
	})

	got, err := secretKeyNonEmpty(context.Background(), client, "openchoreo-observability-plane", "rca-agent-secret", "RCA_LLM_API_KEY")
	if !errors.Is(err, denied) {
		t.Errorf("secretKeyNonEmpty() error = %v, want %v", err, denied)
	}
	if got {
		t.Errorf("secretKeyNonEmpty() = true on an unreadable Secret, want false")
	}
}
