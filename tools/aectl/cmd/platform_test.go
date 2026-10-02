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
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// TestPollAlterPostgresRolePassword_RetriesThroughColdStart covers the race
// alterPostgresPasswordRetryWindow exists for: the official postgres image
// keeps the pod Running through initdb and a temporary internal server
// before the real one accepts connections, so the ALTER can fail a few
// times before it succeeds. This chart's postgres StatefulSet defines no
// readinessProbe (Ready and Running are the same signal), so retrying the
// ALTER itself — not waiting for a Ready that never differs from Running —
// is what actually covers this window.
func TestPollAlterPostgresRolePassword_RetriesThroughColdStart(t *testing.T) {
	orig := runAlterPostgresRolePassword
	t.Cleanup(func() { runAlterPostgresRolePassword = orig })

	var calls int
	runAlterPostgresRolePassword = func(_ context.Context, _, _, _ string) ([]byte, error) {
		calls++
		if calls < 3 {
			return []byte("psql: error: connection to server failed: the database system is starting up"), errors.New("exit status 2")
		}
		return nil, nil
	}

	err := pollAlterPostgresRolePassword(context.Background(), "ns", "pw", time.Now().Add(time.Second), time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls < 3 {
		t.Errorf("expected the ALTER to be retried at least 3 times before succeeding, got %d attempt(s)", calls)
	}
}

// TestPollAlterPostgresRolePassword_TimesOutOnPersistentFailure verifies a
// failure that never clears — not just a transient cold-start one — is
// still surfaced as an error once the retry window elapses, rather than
// retrying forever.
func TestPollAlterPostgresRolePassword_TimesOutOnPersistentFailure(t *testing.T) {
	orig := runAlterPostgresRolePassword
	t.Cleanup(func() { runAlterPostgresRolePassword = orig })

	var calls int
	runAlterPostgresRolePassword = func(_ context.Context, _, _, _ string) ([]byte, error) {
		calls++
		return []byte("boom"), errors.New("exit status 2")
	}

	err := pollAlterPostgresRolePassword(context.Background(), "ns", "pw", time.Now().Add(5*time.Millisecond), time.Millisecond)
	if err == nil {
		t.Fatal("expected an error once the retry window elapses, got nil")
	}
	if calls < 2 {
		t.Errorf("expected more than one attempt before timing out, got %d", calls)
	}
}

func TestAEStudioOverrides(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("tls.enabled", false)
	viper.Set("console.public_url", "http://console.ae.localhost:8080")
	viper.Set("gateway.hostname", "openchoreoapis.localhost")
	viper.Set("thunder.public_url", "http://thunder.openchoreo.localhost:8080")
	viper.Set("thunder.url", "http://thunder-service.thunder.svc.cluster.local:8090")
	viper.Set("thunder.namespace", "wso2-thunder")
	got := strings.Join(aeStudioOverrides(), " ")
	for _, want := range []string{"aeStudio.publicScheme=http", "aeStudio.listenerName=http", "aeStudio.publicPortSuffix=:19080",
		"aeStudio.consoleOrigins={http://console.ae.localhost:8080,http://localhost:8090}", "aeStudio.gatewayHost=openchoreoapis.localhost",
		"aeStudio.idp.issuer=http://thunder.openchoreo.localhost:8080", "aeStudio.idp.jwksUrl=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks",
		"aeStudio.idp.tokenUrl=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token", "--set-json aeStudio.extraEgress=[", `"kubernetes.io/metadata.name":"wso2-thunder"`, `"port":8090`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	viper.Set("tls.enabled", true)
	got = strings.Join(aeStudioOverrides(), " ")
	if !strings.Contains(got, "aeStudio.publicScheme=https") || !strings.Contains(got, "aeStudio.publicPortSuffix=:19443") || !strings.Contains(got, "aeStudio.listenerName=https") {
		t.Fatal(got)
	}
}

func TestAEStudioOverrides_NoThunderURLOmitsIdPURLs(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("thunder.url", "")
	got := strings.Join(aeStudioOverrides(), " ")
	for _, bad := range []string{"idp.jwksUrl", "idp.tokenUrl", "idp.issuer"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s must be absent when thunder.url is empty: %s", bad, got)
		}
	}
}

// A store seeded before ae-studio-internal existed is topped up with only the
// missing generated key; existing generated keys are never rewritten.
func TestSeedMissingGeneratedSecrets_OnlyMissing(t *testing.T) {
	have := map[string]bool{}
	for _, n := range generatedThunderClientNames {
		if n != "ae-studio-internal" {
			have["aep/thunder-clients/"+n] = true
		}
	}
	var wrote []string
	seeded, err := seedMissingGeneratedSecrets(
		func(p string) (bool, error) { return have[p], nil },
		func(p, v string) error {
			if v == "" {
				t.Errorf("empty value for %s", p)
			}
			wrote = append(wrote, p)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 1 || wrote[0] != "aep/thunder-clients/ae-studio-internal" || len(seeded) != 1 {
		t.Fatalf("wrote %v, seeded %v", wrote, seeded)
	}
}

func TestAEStudioImageOverrides(t *testing.T) {
	got := strings.Join(aeStudioImageOverrides("a:1", "", "c:3"), " ")
	if got != "--set aeStudio.images.designAgent=a:1 --set aeStudio.images.studioTools=c:3" {
		t.Fatal(got)
	}
	if aeStudioImageOverrides("", "", "") != nil {
		t.Fatal("empty refs must add nothing")
	}
}

func TestPortOfURL(t *testing.T) {
	for raw, want := range map[string]int{
		"http://thunder:8090":  8090,
		"http://thunder":       80,
		"https://thunder":      443,
		"https://thunder:8443": 8443,
		"":                     8090,
		"thunder":              8090,
	} {
		if got := portOfURL(raw, 8090); got != want {
			t.Errorf("portOfURL(%q) = %d, want %d", raw, got, want)
		}
	}
}

// The AE-only client is the one optional client: its Secret may be missing
// without blocking the others, and aep-thunder-secrets never is.
func TestSecretOptional(t *testing.T) {
	if !secretOptional(aeStudioInternalSecretsName) {
		t.Error("the AE-only Secret must be optional")
	}
	if secretOptional(thunderSecretsName) {
		t.Error("aep-thunder-secrets must be required")
	}
}
