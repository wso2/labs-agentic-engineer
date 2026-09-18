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
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"gopkg.in/yaml.v3"
)

func TestGatewayNaming(t *testing.T) {
	if got, want := gatewayNamespace("acme", "prod"), "acme-prod"; got != want {
		t.Errorf("gatewayNamespace = %q, want %q", got, want)
	}
	if got, want := gatewayRelease("acme", "prod"), "api-platform-acme-prod"; got != want {
		t.Errorf("gatewayRelease = %q, want %q", got, want)
	}
	// env FIRST, org second — the chart derives its kgateway hostname this
	// way (see setup-environment-gateway.sh's own comment on why swapping the
	// order silently breaks the registered vhost).
	if got, want := gatewayHostname("acme", "prod"), "prod-acme.gateway.localhost"; got != want {
		t.Errorf("gatewayHostname = %q, want %q", got, want)
	}
	if got, want := gatewayVhost("acme", "prod"), "http://prod-acme.gateway.localhost:19080"; got != want {
		t.Errorf("gatewayVhost = %q, want %q", got, want)
	}
	if got, want := gatewayBackendJWTSecretName("api-platform-acme-prod"), "api-platform-acme-prod-backend-jwt"; got != want {
		t.Errorf("gatewayBackendJWTSecretName = %q, want %q", got, want)
	}
}

func TestEnsureEncryptionKey_GeneratesAnd32Bytes(t *testing.T) {
	client := fake.NewClientset()
	c := clients{k8s: client}

	key, err := ensureEncryptionKey(context.Background(), c, "ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32 (AES-256)", len(key))
	}
}

func TestEnsureEncryptionKey_PreservesExisting(t *testing.T) {
	existing := []byte("0123456789abcdef0123456789abcdef")[:32]
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayEncryptionSecretName, Namespace: "ns"},
		Data:       map[string][]byte{gatewayEncryptionSecretKey: existing},
	})
	c := clients{k8s: client}

	key, err := ensureEncryptionKey(context.Background(), c, "ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(key) != string(existing) {
		t.Error("ensureEncryptionKey rotated an existing key instead of preserving it")
	}
}

func TestEnsureBackendJWTKeypair_GeneratesValidCertAndKey(t *testing.T) {
	client := fake.NewClientset()
	c := clients{k8s: client}

	assert, keyPEM, err := ensureBackendJWTKeypair(context.Background(), c, "ns", "api-platform-acme-prod", "acme", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assert.issuer != "aep-gateway-acme-prod" {
		t.Errorf("issuer = %q, want aep-gateway-acme-prod", assert.issuer)
	}
	if assert.header != "x-jwt-assertion" {
		t.Errorf("header = %q, want x-jwt-assertion", assert.header)
	}

	certBlock, _ := pem.Decode([]byte(assert.certificate))
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		t.Fatalf("certificate is not a valid PEM CERTIFICATE block: %q", assert.certificate)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	if cert.Subject.CommonName != assert.issuer {
		t.Errorf("certificate CN = %q, want %q", cert.Subject.CommonName, assert.issuer)
	}

	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		t.Fatalf("key is not a valid PEM RSA PRIVATE KEY block: %q", keyPEM)
	}
}

func TestEnsureBackendJWTKeypair_PreservesExisting(t *testing.T) {
	client := fake.NewClientset()
	c := clients{k8s: client}

	assert1, key1, err := ensureBackendJWTKeypair(context.Background(), c, "ns", "api-platform-acme-prod", "acme", "prod")
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	assert2, key2, err := ensureBackendJWTKeypair(context.Background(), c, "ns", "api-platform-acme-prod", "acme", "prod")
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if assert1.certificate != assert2.certificate || key1 != key2 {
		t.Error("ensureBackendJWTKeypair rotated an existing keypair instead of preserving it")
	}
}

func TestRenderGatewayValues(t *testing.T) {
	inst := &ThunderInstance{
		PublicURL: "http://default-idp.openchoreo.localhost:8080",
		AdminURL:  "http://thunder-default-default-service.thunder-default-default.svc.cluster.local:8090",
	}
	assert := assertion{issuer: "aep-gateway-default-default", header: "x-jwt-assertion"}
	values, err := renderGatewayValues("default", "default", "default-default", inst, assert, "-----BEGIN RSA PRIVATE KEY-----\nABC\n-----END RSA PRIVATE KEY-----\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(values), &parsed); err != nil {
		t.Fatalf("rendered values is not valid YAML: %v\n%s", err, values)
	}

	apiGateway, _ := parsed["apiGateway"].(map[string]any)
	if apiGateway == nil || apiGateway["namespace"] != "default-default" {
		t.Errorf("apiGateway.namespace = %v, want default-default", apiGateway["namespace"])
	}

	bootstrap, _ := parsed["bootstrap"].(map[string]any)
	if bootstrap == nil || bootstrap["enabled"] != false {
		t.Errorf("bootstrap.enabled = %v, want false (this package never registers with Agent Manager)", bootstrap["enabled"])
	}
	if _, has := bootstrap["identityProviders"]; has {
		t.Error("bootstrap.identityProviders present, want absent when bootstrap.enabled is false")
	}

	if !strings.Contains(values, "issuer: http://default-idp.openchoreo.localhost:8080") {
		t.Errorf("rendered values missing ThunderKeyManager issuer:\n%s", values)
	}
	if !strings.Contains(values, "uri: http://thunder-default-default-service.thunder-default-default.svc.cluster.local:8090/oauth2/jwks") {
		t.Errorf("rendered values missing ThunderKeyManager JWKS URI:\n%s", values)
	}
	if !strings.Contains(values, "issuer: aep-gateway-default-default") {
		t.Errorf("rendered values missing backend-JWT assertion issuer:\n%s", values)
	}
	if !strings.Contains(values, "-----BEGIN RSA PRIVATE KEY-----") {
		t.Errorf("rendered values missing the indented signing key block:\n%s", values)
	}
}

func TestIndentBlock(t *testing.T) {
	got := indentBlock("a\nb\nc", "  ")
	want := "  a\n  b\n  c"
	if got != want {
		t.Errorf("indentBlock = %q, want %q", got, want)
	}
}
