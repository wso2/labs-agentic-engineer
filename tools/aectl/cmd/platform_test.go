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
	"fmt"
	"regexp"
	"slices"
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
	got := strings.Join(aeStudioOverrides("wso2-aep"), " ")
	for _, want := range []string{"aeStudio.publicScheme=http", "aeStudio.listenerName=http", "aeStudio.publicPortSuffix=:19080",
		"aeStudio.consoleOrigins={http://console.ae.localhost:8080,http://localhost:8090}", "aeStudio.gatewayHost=openchoreoapis.localhost",
		"aeStudio.idp.issuer=http://thunder.openchoreo.localhost:8080", "aeStudio.idp.jwksUrl=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks",
		"aeStudio.idp.tokenUrl=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token", "--set-json aeStudio.extraEgress=[", `"kubernetes.io/metadata.name":"wso2-thunder"`, `"port":8090`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	viper.Set("tls.enabled", true)
	got = strings.Join(aeStudioOverrides("wso2-aep"), " ")
	if !strings.Contains(got, "aeStudio.publicScheme=https") || !strings.Contains(got, "aeStudio.publicPortSuffix=:19443") || !strings.Contains(got, "aeStudio.listenerName=https") {
		t.Fatal(got)
	}
}

func TestAEStudioOverrides_NoThunderURLOmitsIdPURLs(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("thunder.url", "")
	got := strings.Join(aeStudioOverrides("wso2-aep"), " ")
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
	if len(wrote) != 1 || wrote[0] != "aep/thunder-clients/ae-studio-internal" || len(seeded) != 1 || seeded[0].value == "" {
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

// setValidUpdateConfig loads a config that passes config.ValidateLoaded and
// restores every update global and viper on cleanup.
func setValidUpdateConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(viper.Reset)
	prevNS, prevSets, prevReset, prevChart := updateNamespace, updateHelmSets, updateResetValues, updatePlatformChart
	t.Cleanup(func() {
		updateNamespace, updateHelmSets, updateResetValues, updatePlatformChart = prevNS, prevSets, prevReset, prevChart
	})
	updateNamespace, updateHelmSets, updateResetValues, updatePlatformChart = "wso2-aep", nil, false, ""
	viper.Set("gateway.hostname", "openchoreoapis.localhost")
	viper.Set("thunder.public_url", "http://thunder.openchoreo.localhost:8080")
	viper.Set("thunder.url", "http://thunder-service.thunder.svc.cluster.local:8090")
	viper.Set("thunder.namespace", "wso2-thunder")
	viper.Set("thunder.admin_client_id", "admin")
	viper.Set("oc.api_url", "http://api.openchoreo.localhost:8080")
	viper.Set("oc.system_namespace", "openchoreo-control-plane")
}

// `platform update` (and so `make dev-update`) must carry every pair install
// derives, or an install that predates them never converges (checkpoint red-B-1).
func TestBuildUpdateArgs_CarriesAEStudioValues(t *testing.T) {
	setValidUpdateConfig(t)

	got, err := buildUpdateArgs()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\x00")
	want := aeStudioOverrides("wso2-aep")
	for i := 0; i+1 < len(want); i += 2 {
		if !strings.Contains(joined, want[i]+"\x00"+want[i+1]) {
			t.Errorf("update args lack %s %s", want[i], want[i+1])
		}
	}
	for _, key := range []string{"aeStudio.gatewayHost=", "aeStudio.idp.issuer=", "aeStudio.idp.jwksUrl=", "aeStudio.idp.tokenUrl=", "aeStudio.consoleOrigins=", "aeStudio.extraEgress="} {
		if !strings.Contains(joined, key) {
			t.Errorf("update args lack %s", key)
		}
	}
}

// Update keeps the release's user-supplied values but renders on the new
// chart's defaults (--reset-then-reuse-values): under --reuse-values Helm
// renders on the defaults of the chart the release was installed with, so a
// default a newer chart adds never reaches an existing install (Task 4.22a:
// aeStudio.webhookRelay.image). --reset-values still opts out of the reuse.
func TestBuildUpdateArgs_ValueStrategy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reset bool
		want  string
	}{
		{"default", false, "--reset-then-reuse-values"},
		{"reset", true, "--reset-values"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setValidUpdateConfig(t)
			updateResetValues = tc.reset
			got, err := buildUpdateArgs()
			if err != nil {
				t.Fatal(err)
			}
			var strategies []string
			for _, a := range got {
				if a == "--reuse-values" || a == "--reset-values" || a == "--reset-then-reuse-values" {
					strategies = append(strategies, a)
				}
			}
			if len(strategies) != 1 || strategies[0] != tc.want {
				t.Errorf("value strategy flags = %v, want exactly [%s]", strategies, tc.want)
			}
		})
	}
}

// The relay switch follows the config on update both ways; the relay image is
// never set by aectl: it is the chart's own default (values.yaml, the single
// source of the gosmee digest), which --reset-then-reuse-values applies.
func TestBuildUpdateArgs_WebhookRelay(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			setValidUpdateConfig(t)
			viper.Set("ae_studio.webhook_relay.enabled", enabled)
			got, err := buildUpdateArgs()
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(got, "\x00")
			if want := fmt.Sprintf("--set\x00aeStudio.webhookRelay.enabled=%t", enabled); !strings.Contains(joined, want) {
				t.Errorf("update args lack %q: %v", want, got)
			}
			if strings.Contains(joined, "aeStudio.webhookRelay.image") {
				t.Errorf("aectl must not set the relay image (chart default is the source): %v", got)
			}
			if !strings.Contains(joined, "--reset-then-reuse-values") {
				t.Errorf("without --reset-then-reuse-values the chart's relay image default never reaches an existing install: %v", got)
			}
		})
	}
}

// Missing or partial aectl config must fail loudly, not derive http/empty
// values the upgrade would write over a working install.
func TestBuildUpdateArgs_RejectsMissingConfig(t *testing.T) {
	setValidUpdateConfig(t)
	viper.Reset()
	if _, err := buildUpdateArgs(); err == nil || !strings.Contains(err.Error(), "thunder.namespace") {
		t.Fatalf("empty config: want error naming thunder.namespace, got %v", err)
	}
	viper.Set("thunder.url", "http://thunder:8090")
	if _, err := buildUpdateArgs(); err == nil || !strings.Contains(err.Error(), "thunder.namespace") {
		t.Fatalf("partial config: want error naming thunder.namespace, got %v", err)
	}
}

// aeStudioOverrides precede the user's --set, so an explicit override wins.
func TestBuildUpdateArgs_UserSetOverridesAEStudio(t *testing.T) {
	setValidUpdateConfig(t)
	updateHelmSets = []string{"aeStudio.gatewayHost=custom.example.com"}
	got, err := buildUpdateArgs()
	if err != nil {
		t.Fatal(err)
	}
	derived, user := -1, -1
	for i, a := range got {
		switch a {
		case "aeStudio.gatewayHost=openchoreoapis.localhost":
			derived = i
		case "aeStudio.gatewayHost=custom.example.com":
			user = i
		}
	}
	if derived < 0 || user < 0 || derived > user {
		t.Fatalf("want derived before user --set, got derived=%d user=%d: %v", derived, user, got)
	}
}

func TestAEStudioOverrides_EmptyGatewayHostOmitted(t *testing.T) {
	t.Cleanup(viper.Reset)
	if got := strings.Join(aeStudioOverrides("wso2-aep"), " "); strings.Contains(got, "gatewayHost") {
		t.Errorf("empty gateway.hostname must not be set (would wipe a reused value): %s", got)
	}
}

// Task 4.20 (Q-11): install and update both carry the relay switch from
// ae_studio.webhook_relay.enabled; absent is false.
func TestAEStudioOverrides_WebhookRelaySwitch(t *testing.T) {
	t.Cleanup(viper.Reset)
	if got := strings.Join(aeStudioOverrides("wso2-aep"), " "); !strings.Contains(got, "--set aeStudio.webhookRelay.enabled=false") {
		t.Errorf("absent key: want the relay off, got %s", got)
	}
	viper.Set("ae_studio.webhook_relay.enabled", true)
	if got := strings.Join(aeStudioOverrides("wso2-aep"), " "); !strings.Contains(got, "--set aeStudio.webhookRelay.enabled=true") {
		t.Errorf("want the relay on, got %s", got)
	}
}

// The relay seed is generated with the install's secrets but is not one of
// the paths whose absence means a wiped store: an install that predates the
// relay is not "wiped".
func TestWebhookRelaySeed_GeneratedNotRequired(t *testing.T) {
	if slices.Contains(requiredOpenBaoPaths, webhookRelaySeedPath) {
		t.Error("aep/webhook-relay-seed must not be in requiredOpenBaoPaths")
	}
	seed, err := webhookRelaySeed()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(seed) {
		t.Errorf("seed must be 32 random bytes as hex text (aectl secret import trims), got %d chars", len(seed))
	}
	if other, _ := webhookRelaySeed(); other == seed {
		t.Error("seed must be random")
	}
}

// An install with --reuse-secrets tops up aep/webhook-relay-seed create-only
// when the relay is on (a store from before the relay has none), leaves an
// existing seed alone, and does not touch the store with the relay off.
func TestReuseSecrets_TopsUpTheRelaySeed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enabled   bool
		present   bool
		wantWrite bool
	}{
		{"relay on, seed absent: written", true, false, true},
		{"relay on, seed present: kept", true, true, false},
		{"relay off: untouched", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(viper.Reset)
			viper.Set("ae_studio.webhook_relay.enabled", tc.enabled)
			var checked, wrote []string
			err := topUpWebhookRelaySeed(
				func(p string) (bool, error) { checked = append(checked, p); return tc.present, nil },
				func(p, v string) error { wrote = append(wrote, p); return nil })
			if err != nil {
				t.Fatal(err)
			}
			if got := len(wrote) == 1 && wrote[0] == webhookRelaySeedPath; got != tc.wantWrite || len(wrote) > 1 {
				t.Fatalf("wrote %v, want a write of the seed: %t", wrote, tc.wantWrite)
			}
			if !tc.enabled && len(checked) != 0 {
				t.Fatalf("relay off read the store: %v", checked)
			}
		})
	}
}
