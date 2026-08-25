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
		ObjectMeta: metav1.ObjectMeta{Name: ThunderAdminCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{"some-other-key": []byte("value")},
	}
	client := fake.NewSimpleClientset(sec)
	viper.Reset()

	err := LoadThunderSecretFromCluster(context.Background(), client, "aep")
	if err == nil {
		t.Fatal("expected error when key is absent, got nil")
	}
	if !strings.Contains(err.Error(), ThunderAdminCredsSecretKey) {
		t.Errorf("error should mention the missing key %q, got: %v", ThunderAdminCredsSecretKey, err)
	}
}

func TestLoadThunderSecretFromCluster_KeyEmpty(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ThunderAdminCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{ThunderAdminCredsSecretKey: {}},
	}
	client := fake.NewSimpleClientset(sec)
	viper.Reset()

	err := LoadThunderSecretFromCluster(context.Background(), client, "aep")
	if err == nil {
		t.Fatal("expected error when key is empty, got nil")
	}
	if !strings.Contains(err.Error(), ThunderAdminCredsSecretKey) {
		t.Errorf("error should mention the empty key %q, got: %v", ThunderAdminCredsSecretKey, err)
	}
}

func TestLoadThunderSecretFromCluster_SetsDefault(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ThunderAdminCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{ThunderAdminCredsSecretKey: []byte("my-secret")},
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
		ObjectMeta: metav1.ObjectMeta{Name: ThunderAdminCredsSecret, Namespace: "aep"},
		Data:       map[string][]byte{ThunderAdminCredsSecretKey: []byte("cluster-secret")},
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

func TestGatewayHostname_InConfigMapKeys(t *testing.T) {
	found := false
	for _, k := range ConfigMapKeys {
		if k == "gateway.hostname" {
			found = true
			break
		}
	}
	if !found {
		t.Error("gateway.hostname must be present in ConfigMapKeys")
	}
}

func TestGatewayHostname_InKeyRegistry(t *testing.T) {
	meta, ok := keyRegistry["gateway.hostname"]
	if !ok {
		t.Fatal("gateway.hostname must be present in keyRegistry")
	}
	if meta.required {
		t.Error("gateway.hostname must be optional (required=false)")
	}
	if meta.kind != kindString {
		t.Errorf("gateway.hostname kind = %v, want kindString", meta.kind)
	}
}

func TestLoadFromCluster_GatewayHostname_Populated(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: "aep"},
		Data:       map[string]string{"gateway.hostname": "myapis.example.com"},
	}
	client := fake.NewSimpleClientset(cm)
	viper.Reset()

	n, err := LoadFromCluster(context.Background(), client, "aep")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n == 0 {
		t.Error("LoadFromCluster should report at least one key loaded")
	}
	if got := viper.GetString("gateway.hostname"); got != "myapis.example.com" {
		t.Errorf("gateway.hostname = %q, want %q", got, "myapis.example.com")
	}
}

func TestLoadFromCluster_GatewayHostname_Absent(t *testing.T) {
	// ConfigMap exists but does not include gateway.hostname.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: "aep"},
		Data:       map[string]string{"thunder.namespace": "wso2-thunder"},
	}
	client := fake.NewSimpleClientset(cm)
	viper.Reset()

	if _, err := LoadFromCluster(context.Background(), client, "aep"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := viper.GetString("gateway.hostname"); got != "" {
		t.Errorf("gateway.hostname should be empty when absent from ConfigMap, got %q", got)
	}
	// Optional key absence must not surface as a validation error.
	for _, e := range ValidateLoaded() {
		if strings.Contains(e, "gateway.hostname") {
			t.Errorf("optional gateway.hostname must not cause a validation error, got: %s", e)
		}
	}
}
