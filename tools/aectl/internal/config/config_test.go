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

package config

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLoadThunderSecretFromCluster_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	viper.Reset()

	if err := LoadThunderSecretFromCluster(context.Background(), client, "aep"); err != nil {
		t.Fatalf("expected nil for missing secret (first-install), got: %v", err)
	}
	if viper.IsSet("thunder.admin_client_secret") {
		t.Error("thunder.admin_client_secret should not be set when secret is absent")
	}
}

func TestLoadThunderSecretFromCluster_KeyMissing(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ThunderOperatorCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{"some-other-key": []byte("value")},
	}
	client := fake.NewSimpleClientset(sec)
	viper.Reset()

	err := LoadThunderSecretFromCluster(context.Background(), client, "aep")
	if err == nil {
		t.Fatal("expected error when key is absent, got nil")
	}
	if !strings.Contains(err.Error(), ThunderOperatorCredsSecretKey) {
		t.Errorf("error should mention the missing key %q, got: %v", ThunderOperatorCredsSecretKey, err)
	}
}

func TestLoadThunderSecretFromCluster_KeyEmpty(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ThunderOperatorCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{ThunderOperatorCredsSecretKey: {}},
	}
	client := fake.NewSimpleClientset(sec)
	viper.Reset()

	err := LoadThunderSecretFromCluster(context.Background(), client, "aep")
	if err == nil {
		t.Fatal("expected error when key is empty, got nil")
	}
	if !strings.Contains(err.Error(), ThunderOperatorCredsSecretKey) {
		t.Errorf("error should mention the empty key %q, got: %v", ThunderOperatorCredsSecretKey, err)
	}
}

func TestLoadThunderSecretFromCluster_SetsDefault(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ThunderOperatorCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{ThunderOperatorCredsSecretKey: []byte("my-secret")},
	}
	client := fake.NewSimpleClientset(sec)
	viper.Reset()

	if err := LoadThunderSecretFromCluster(context.Background(), client, "aep"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := viper.GetString("thunder.admin_client_secret"); got != "my-secret" {
		t.Errorf("thunder.admin_client_secret = %q, want %q", got, "my-secret")
	}
}

func TestLoadThunderSecretFromCluster_EnvOverridesDefault(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ThunderOperatorCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{ThunderOperatorCredsSecretKey: []byte("cluster-secret")},
	}
	client := fake.NewSimpleClientset(sec)
	viper.Reset()
	viper.Set("thunder.admin_client_secret", "override-secret")

	if err := LoadThunderSecretFromCluster(context.Background(), client, "aep"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// viper.SetDefault does not override an already-set value.
	if got := viper.GetString("thunder.admin_client_secret"); got != "override-secret" {
		t.Errorf("thunder.admin_client_secret = %q, want %q (SetDefault must not clobber existing value)", got, "override-secret")
	}
}
