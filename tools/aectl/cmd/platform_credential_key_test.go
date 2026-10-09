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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// The cluster Secret is the copy ensureCredentialEncryptionKey restores from
// when OpenBao has lost the key; reading the wrong value (or treating an absent
// Secret as an error) would mint a new key over a database sealed under the old.
func TestCredentialEncryptionKeyFromCluster(t *testing.T) {
	const ns = "wso2-aep"
	const key = "c3RvcmVkLWtleS1zdG9yZWQta2V5LXN0b3JlZC1rZXk="

	tests := []struct {
		name    string
		objects []corev1.Secret
		want    string
		wantErr bool
	}{
		{
			name: "secret holds the key",
			objects: []corev1.Secret{{
				ObjectMeta: metav1.ObjectMeta{Name: credentialEncryptionKeySecret, Namespace: ns},
				Data:       map[string][]byte{"CREDENTIAL_ENCRYPTION_KEY": []byte(key)},
			}},
			want: key,
		},
		{name: "no secret — fresh installation", want: ""},
		{
			name: "secret without the key",
			objects: []corev1.Secret{{
				ObjectMeta: metav1.ObjectMeta{Name: credentialEncryptionKeySecret, Namespace: ns},
			}},
			want: "",
		},
		// A malformed value must stop the run, not be restored into OpenBao
		// (aep-api would refuse it) or fall through to generating a new key.
		{
			name: "not base64",
			objects: []corev1.Secret{{
				ObjectMeta: metav1.ObjectMeta{Name: credentialEncryptionKeySecret, Namespace: ns},
				Data:       map[string][]byte{"CREDENTIAL_ENCRYPTION_KEY": []byte("!!!not-base64!!!")},
			}},
			wantErr: true,
		},
		{
			name: "16 bytes, not 32",
			objects: []corev1.Secret{{
				ObjectMeta: metav1.ObjectMeta{Name: credentialEncryptionKeySecret, Namespace: ns},
				Data:       map[string][]byte{"CREDENTIAL_ENCRYPTION_KEY": []byte("MDEyMzQ1Njc4OWFiY2RlZg==")},
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			for i := range tt.objects {
				if _, err := client.CoreV1().Secrets(ns).Create(context.Background(), &tt.objects[i], metav1.CreateOptions{}); err != nil {
					t.Fatalf("seed secret: %v", err)
				}
			}
			got, err := credentialEncryptionKeyFromCluster(context.Background(), client, ns)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got %q, nil; want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("credentialEncryptionKeyFromCluster: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q; want %q", got, tt.want)
			}
		})
	}
}
