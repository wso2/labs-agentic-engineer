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
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReleaseNameAndURLs(t *testing.T) {
	if got, want := releaseName("acme", "prod"), "thunder-acme-prod"; got != want {
		t.Errorf("releaseName = %q, want %q", got, want)
	}
	if got, want := publicURL("prod"), "http://prod-idp.openchoreo.localhost:8080"; got != want {
		t.Errorf("publicURL = %q, want %q", got, want)
	}
	if got, want := adminURL("thunder-acme-prod", "thunder-acme-prod"), "http://thunder-acme-prod-service.thunder-acme-prod.svc.cluster.local:8090"; got != want {
		t.Errorf("adminURL = %q, want %q", got, want)
	}
	if got, want := systemClientSecretName("thunder-acme-prod"), "thunder-acme-prod-aep-system-client"; got != want {
		t.Errorf("systemClientSecretName = %q, want %q", got, want)
	}
}

func TestReleaseName_BoundaryLength(t *testing.T) {
	// "thunder-" (8) + org + "-" (1) + env == exactly maxReleaseName: still
	// verbatim, no hash suffix.
	org := strings.Repeat("o", 22)
	env := strings.Repeat("e", 22)
	natural := "thunder-" + org + "-" + env
	if len(natural) != maxReleaseName {
		t.Fatalf("test fixture is %d chars, want exactly %d", len(natural), maxReleaseName)
	}
	if got := releaseName(org, env); got != natural {
		t.Errorf("releaseName at exactly the limit = %q, want verbatim %q", got, natural)
	}

	// One character over: must shrink to the bound and gain a hash suffix.
	over := releaseName(org, env+"e")
	if len(over) > maxReleaseName {
		t.Errorf("releaseName one char over the limit = %q (%d chars), want <= %d", over, len(over), maxReleaseName)
	}
	if over == natural {
		t.Errorf("releaseName one char over the limit did not change from the boundary case")
	}
}

func TestReleaseName_NoCollisionOnSharedPrefix(t *testing.T) {
	// Two long (org, env) pairs sharing every character up to the truncation
	// point must still produce different release names — the whole point of
	// hashing the FULL natural name rather than just truncating it.
	longOrg := strings.Repeat("x", 40)
	a := releaseName(longOrg, "environment-one")
	b := releaseName(longOrg, "environment-two")
	if a == b {
		t.Errorf("releaseName collided for two different long inputs: both = %q", a)
	}
	if len(a) > maxReleaseName || len(b) > maxReleaseName {
		t.Errorf("releaseName exceeded the limit: %q (%d), %q (%d)", a, len(a), b, len(b))
	}
}

func TestValidReleaseName(t *testing.T) {
	if err := validReleaseName(releaseName("acme", "prod")); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	for _, name := range []string{"Acme_Prod", "thunder-default-development-", "-thunder-default"} {
		if err := validReleaseName(name); err == nil {
			t.Errorf("validReleaseName(%q) = nil, want an error", name)
		}
	}
}

func TestResolveSystemClientSecret_GeneratesWhenMissing(t *testing.T) {
	client := fake.NewClientset()
	c := clients{k8s: client}

	secret, err := resolveSystemClientSecret(context.Background(), c, "ns", "thunder-a-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret == "" {
		t.Fatal("expected a generated secret, got empty string")
	}
	if len(secret) != 48 { // hex.EncodeToString of 24 random bytes
		t.Errorf("generated secret length = %d, want 48 (hex of 24 bytes)", len(secret))
	}
}

func TestResolveSystemClientSecret_ReusesExisting(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "thunder-a-b-aep-system-client", Namespace: "ns"},
		Data:       map[string][]byte{"client-secret": []byte("already-set")},
	})
	c := clients{k8s: client}

	secret, err := resolveSystemClientSecret(context.Background(), c, "ns", "thunder-a-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret != "already-set" {
		t.Errorf("secret = %q, want the existing value %q (must never rotate)", secret, "already-set")
	}
}

func TestResolveSystemClientSecret_ErrorsOnEmptyExistingKey(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "thunder-a-b-aep-system-client", Namespace: "ns"},
		Data:       map[string][]byte{"some-other-key": []byte("x")},
	})
	c := clients{k8s: client}

	if _, err := resolveSystemClientSecret(context.Background(), c, "ns", "thunder-a-b"); err == nil {
		t.Fatal("expected an error when the existing Secret carries no client-secret key")
	}
}

func TestThunderChartSpec(t *testing.T) {
	inst := &ThunderInstance{
		Release:   "thunder-default-default",
		Namespace: "thunder-default-default",
		PublicURL: "http://default-idp.openchoreo.localhost:8080",
	}
	spec, err := thunderChartSpec(inst, "thunder-default-default-bootstrap", []string{"13-fix.yaml", "80-client.yaml", "84-role.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.ReleaseName != inst.Release {
		t.Errorf("ReleaseName = %q, want %q", spec.ReleaseName, inst.Release)
	}
	if spec.Chart != thunderChart {
		t.Errorf("Chart = %q, want %q", spec.Chart, thunderChart)
	}
	if spec.Namespace != inst.Namespace {
		t.Errorf("Namespace = %q, want %q", spec.Namespace, inst.Namespace)
	}

	wantSets := []string{
		"configuration.database.config.type=sqlite",
		"configuration.database.runtime_transient.type=sqlite",
		"configuration.database.entity.type=sqlite",
		"configuration.database.runtime_persistent.type=sqlite",
		"bootstrap.configMap.name=thunder-default-default-bootstrap",
	}
	for _, want := range wantSets {
		if !containsString(spec.Sets, want) {
			t.Errorf("Sets %v missing %q", spec.Sets, want)
		}
	}

	wantSetStrings := []string{
		"fullnameOverride=thunder-default-default",
		"httproute.hostnames[0]=default-idp.openchoreo.localhost",
		"configuration.server.publicUrl=http://default-idp.openchoreo.localhost:8080",
		"configuration.jwt.issuer=http://default-idp.openchoreo.localhost:8080",
	}
	for _, want := range wantSetStrings {
		if !containsString(spec.SetStrings, want) {
			t.Errorf("SetStrings %v missing %q", spec.SetStrings, want)
		}
	}

	if len(spec.SetJSON) != 1 {
		t.Fatalf("expected exactly one --set-json entry, got %v", spec.SetJSON)
	}
	prefix := "bootstrap.configMap.files="
	if !strings.HasPrefix(spec.SetJSON[0], prefix) {
		t.Fatalf("SetJSON[0] = %q, want prefix %q", spec.SetJSON[0], prefix)
	}
	var files []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(spec.SetJSON[0], prefix)), &files); err != nil {
		t.Fatalf("SetJSON[0] is not valid JSON: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("bootstrap file list = %v, want 3 entries", files)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
