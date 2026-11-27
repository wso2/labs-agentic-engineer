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
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/wso2/aep/aectl/internal/idpclaims"
	"github.com/wso2/aep/aectl/internal/oidc"
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

// The secret store's target plane reaches the chart only when config names
// it; unset leaves the chart default (ClusterDataPlane/default).
func TestSecretStoreOverrides(t *testing.T) {
	for name, tc := range map[string]struct {
		kind, plane string
		want        []string
	}{
		"neither set": {"", "", nil},
		"both set": {"DataPlane", "edge", []string{
			"--set", "secretStore.targetPlane.kind=DataPlane",
			"--set", "secretStore.targetPlane.name=edge",
		}},
		"only the name": {"", "edge", []string{"--set", "secretStore.targetPlane.name=edge"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := secretStoreOverrides(tc.kind, tc.plane); !slices.Equal(got, tc.want) {
				t.Fatalf("secretStoreOverrides(%q, %q) = %v, want %v", tc.kind, tc.plane, got, tc.want)
			}
		})
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
		"--set-json aeStudio.extraEgress=[", `"kubernetes.io/metadata.name":"wso2-thunder"`, `"port":8090`} {
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

// testPlatformIDP is the local ThunderID as discovery names it, with the
// in-cluster endpoints on thunder.url: the values every install so far wrote
// by appending the paths by hand.
// presetClaims is the idp.claims block of a shipped preset with nothing laid
// over it, for fixtures.
func presetClaims(name string) idpclaims.Resolved {
	r, err := idpclaims.Resolve(name, func(string) (string, idpclaims.Source) { return "", "" })
	if err != nil {
		panic(err)
	}
	return r
}

var testPlatformIDP = platformIDP{
	Clients:      "thunder",
	Claims:       presetClaims("thunder"),
	OrgHandle:    "default",
	OrgNamespace: "default",
	Public: oidc.Endpoints{
		Issuer:   "http://thunder.openchoreo.localhost:8080",
		JWKSURL:  "http://thunder.openchoreo.localhost:8080/oauth2/jwks",
		TokenURL: "http://thunder.openchoreo.localhost:8080/oauth2/token",
	},
	InCluster: oidc.Endpoints{
		Issuer:   "http://thunder.openchoreo.localhost:8080",
		JWKSURL:  "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks",
		TokenURL: "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token",
	},
}

// textLikeClaims is an idp.claims block whose values look like another type
// to Helm: a boolean, a number, and an empty optional value.
func textLikeClaims(t *testing.T) idpclaims.Resolved {
	t.Helper()
	set := map[string]string{"org.handle": "123", "caller_type.machine": "true"}
	r, err := idpclaims.Resolve("thunder", func(key string) (string, idpclaims.Source) {
		if v, ok := set[key]; ok {
			return v, idpclaims.SourceConfig
		}
		return "", ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A claim's name or value is text that the IdP's admin chose. Helm's --set
// turns "true" into a boolean and "123" into a number, so every claim value
// goes by --set-string, and none by --set.
func TestPlatformIDPOverrides_ClaimValuesStayText(t *testing.T) {
	idp := testPlatformIDP
	idp.Claims = textLikeClaims(t)
	args := platformIDPOverrides(idp)
	claims := 0
	for i := 0; i+1 < len(args); i += 2 {
		if !strings.HasPrefix(args[i+1], "idp.claims.") {
			continue
		}
		claims++
		if args[i] != "--set-string" {
			t.Errorf("%s is passed with %s, want --set-string", args[i+1], args[i])
		}
	}
	if claims != len(idpclaims.Fields) {
		t.Fatalf("%d claim values passed, want %d: %v", claims, len(idpclaims.Fields), args)
	}
	got := strings.Join(args, " ")
	for _, want := range []string{
		"--set-string idp.claims.org.handle=123",
		"--set-string idp.claims.callerType.machine=true",
		"--set-string idp.claims.callerType.user=",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

// The same values through the chart: Helm renders each as quoted text. With
// --set the render stopped, because the chart trims a boolean or a number.
func TestPlatformIDPOverrides_ClaimValuesRenderAsText(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not in PATH: the render of the chart is not checked")
	}
	idp := testPlatformIDP
	idp.Claims = textLikeClaims(t)
	args := append([]string{"template", "t", filepath.Join("..", "..", "..", "deployments", "helm-charts", "platform")}, platformIDPOverrides(idp)...)
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	rendered := string(out)
	for name, value := range map[string]string{
		"PLATFORM_IDP_CLAIMS_ORG_HANDLE":          `"123"`,
		"PLATFORM_IDP_CLAIMS_CALLER_TYPE_MACHINE": `"true"`,
		"PLATFORM_IDP_CLAIMS_CALLER_TYPE_USER":    `""`,
		"VITE_IDP_CLAIMS_ORG_HANDLE":              `"123"`,
	} {
		re := regexp.MustCompile(`name: ` + name + `\s+value: ` + regexp.QuoteMeta(value) + `\s`)
		if !re.MatchString(rendered) {
			t.Errorf("%s is not rendered as the text %s", name, value)
		}
	}
}

// The IdP values the chart gets are the discovered endpoints: the issuer as
// named, the JWKS and token endpoints at the in-cluster address, and the
// kind every reader is told. The strings equal what the install wrote before
// discovery, which is the no-behaviour-change proof.
func TestPlatformIDPOverrides(t *testing.T) {
	got := strings.Join(platformIDPOverrides(testPlatformIDP), " ")
	for _, want := range []string{
		"--set idp.clients=thunder",
		"--set-string idp.claims.org.handle=ouHandle",
		"--set-string idp.claims.org.id=ouId",
		"--set-string idp.claims.org.name=ouName",
		"--set-string idp.claims.callerType.claim=grant_type",
		"--set-string idp.claims.callerType.machine=client_credentials",
		"--set idp.jwksURL=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks",
		"--set idp.tokenURL=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token",
		"--set aeStudio.idp.issuer=http://thunder.openchoreo.localhost:8080",
		"--set aeStudio.idp.jwksUrl=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks",
		"--set aeStudio.idp.tokenUrl=http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(strings.Join(aeStudioOverrides("wso2-aep"), " "), "idp.") {
		t.Error("the IdP values belong to platformIDPOverrides alone")
	}
}

// Undiscovered endpoints set no IdP value: the chart keeps what it has rather
// than taking half-built URLs.
func TestPlatformIDPOverrides_UndiscoveredSetsNothing(t *testing.T) {
	if got := platformIDPOverrides(platformIDP{Clients: "thunder"}); len(got) != 0 {
		t.Errorf("want nothing, got %v", got)
	}
}

// discoverPlatformIDP reads the document over thunder.public_url and puts the
// same paths on thunder.url for the in-cluster endpoints; a document that
// cannot be had is an error, never hand-built URLs.
func TestDiscoverPlatformIDP(t *testing.T) {
	t.Cleanup(viper.Reset)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"jwks_uri":%q,"token_endpoint":%q}`, srvURL(r), srvURL(r)+"/oauth2/jwks", srvURL(r)+"/oauth2/token")
	}))
	t.Cleanup(srv.Close)
	viper.Set("thunder.public_url", srv.URL)
	viper.Set("thunder.url", "http://thunder-service.thunder.svc.cluster.local:8090")
	idp, err := discoverPlatformIDP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if idp.Claims.Preset != "thunder" || idp.Claims.Claims().OrgHandle != "ouHandle" || idp.Public.Issuer != srv.URL || idp.Public.JWKSURL != srv.URL+"/oauth2/jwks" {
		t.Fatalf("public: %+v", idp)
	}
	if idp.InCluster.JWKSURL != "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks" ||
		idp.InCluster.TokenURL != "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token" {
		t.Fatalf("in-cluster: %+v", idp.InCluster)
	}
	viper.Set("thunder.url", "")
	idp, err = discoverPlatformIDP(context.Background())
	if err != nil || idp.InCluster != idp.Public {
		t.Fatalf("without thunder.url the in-cluster endpoints are the public ones: %+v, %v", idp, err)
	}
	srv.Close()
	if _, err := discoverPlatformIDP(context.Background()); err == nil {
		t.Fatal("an unreachable IdP must be an error")
	}
}

// srvURL is the request's own server address, so the served document names
// the issuer it is fetched from.
func srvURL(r *http.Request) string { return "http://" + r.Host }

// A store seeded before ae-studio-internal existed is topped up with only the
// missing generated key; existing generated keys are never rewritten.
func TestSeedGeneratedThunderClientSecrets_OnlyMissing(t *testing.T) {
	have := map[string]bool{}
	for _, n := range generatedThunderClientNames {
		if n != "ae-studio-internal" {
			have["aep/idp-clients/"+n] = true
		}
	}
	var wrote []string
	seeded, err := seedGeneratedSecrets(generatedThunderClientSecrets(), seedStore{
		exists: func(p string) (bool, error) { return have[p], nil },
		put: func(p, v string) error {
			if v == "" {
				t.Errorf("empty value for %s", p)
			}
			wrote = append(wrote, p)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 1 || wrote[0] != "aep/idp-clients/ae-studio-internal" || len(seeded) != 1 || seeded[0].value == "" {
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
// without blocking the others, and aep-idp-client-secrets never is.
func TestSecretOptional(t *testing.T) {
	if !secretOptional(aeStudioInternalSecretsName) {
		t.Error("the AE-only Secret must be optional")
	}
	if secretOptional(thunderSecretsName) {
		t.Error("aep-idp-client-secrets must be required")
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
	viper.Set("idp.org_handle", "default")
}

// `platform update` (and so `make dev-update`) must carry every pair install
// derives, or an install that predates them never converges (checkpoint red-B-1).
func TestHelmUpgradeArgs_CarriesAEStudioValues(t *testing.T) {
	setValidUpdateConfig(t)

	cfg := updateConfigFromFlags()
	cfg.IDP = testPlatformIDP
	got, err := helmUpgradeArgs(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\x00")
	want := append(aeStudioOverrides("wso2-aep"), platformIDPOverrides(testPlatformIDP)...)
	for i := 0; i+1 < len(want); i += 2 {
		if !strings.Contains(joined, want[i]+"\x00"+want[i+1]) {
			t.Errorf("update args lack %s %s", want[i], want[i+1])
		}
	}
	for _, key := range []string{"aeStudio.gatewayHost=", "aeStudio.idp.issuer=", "aeStudio.idp.jwksUrl=", "aeStudio.idp.tokenUrl=", "idp.clients=", "idp.claims.org.handle=", "idp.claims.callerType.user=", "idp.tokenURL=", "aeStudio.consoleOrigins=", "aeStudio.extraEgress="} {
		if !strings.Contains(joined, key) {
			t.Errorf("update args lack %s", key)
		}
	}
}

// Update keeps the release's user-supplied values but renders on the new
// chart's defaults (--reset-then-reuse-values): under --reuse-values Helm
// renders on the defaults of the chart the release was installed with, so a
// default a newer chart adds never reaches an existing install (for example
// aeStudio.webhookRelay.image). --reset-values still opts out of the reuse.
func TestHelmUpgradeArgs_ValueStrategy(t *testing.T) {
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
			got, err := helmUpgradeArgs(updateConfigFromFlags())
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
func TestHelmUpgradeArgs_WebhookRelay(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			setValidUpdateConfig(t)
			viper.Set("ae_studio.webhook_relay.enabled", enabled)
			got, err := helmUpgradeArgs(updateConfigFromFlags())
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

// The Jev switch and endpoint come from the aectl config on install and on
// every update, both ways, so neither a `make dev-env` (a helm install that
// resets values) nor a later update drops a Jev an operator turned on.
func TestAEStudioOverrides_JevFollowsConfig(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		url     string
	}{{true, "https://jev.example/v1"}, {false, ""}} {
		t.Run(fmt.Sprintf("enabled=%t", tc.enabled), func(t *testing.T) {
			setValidUpdateConfig(t)
			viper.Set("ae_studio.jev.enabled", tc.enabled)
			viper.Set("ae_studio.jev.url", tc.url)
			got, err := helmUpgradeArgs(updateConfigFromFlags())
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(got, "\x00")
			for _, want := range []string{
				fmt.Sprintf("--set\x00aeStudio.jev.enabled=%t", tc.enabled),
				"--set\x00aeStudio.jev.url=" + tc.url,
			} {
				if !strings.Contains(joined, want) {
					t.Errorf("args lack %q: %v", want, got)
				}
			}
		})
	}
}

// Missing or partial aectl config must fail loudly, not derive http/empty
// values the upgrade would write over a working install. platformUpdate runs
// this check first, for `platform update` and `sre install` alike.
func TestRequireAEStudioConfig_RejectsMissingConfig(t *testing.T) {
	setValidUpdateConfig(t)
	if err := requireAEStudioConfig(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	viper.Reset()
	if err := requireAEStudioConfig(); err == nil || !strings.Contains(err.Error(), "thunder.namespace") {
		t.Fatalf("empty config: want error naming thunder.namespace, got %v", err)
	}
	viper.Set("thunder.url", "http://thunder:8090")
	if err := requireAEStudioConfig(); err == nil || !strings.Contains(err.Error(), "thunder.namespace") {
		t.Fatalf("partial config: want error naming thunder.namespace, got %v", err)
	}
}

// aeStudioOverrides precede the user's --set, so an explicit override wins.
func TestHelmUpgradeArgs_UserSetOverridesAEStudio(t *testing.T) {
	setValidUpdateConfig(t)
	updateHelmSets = []string{"aeStudio.gatewayHost=custom.example.com"}
	got, err := helmUpgradeArgs(updateConfigFromFlags())
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

// Install and update both carry the relay switch from
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
	if slices.Contains(requiredOpenBaoPaths(), webhookRelaySeedPath) {
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
			err := topUpWebhookRelaySeed(seedStore{
				exists: func(p string) (bool, error) { checked = append(checked, p); return tc.present, nil },
				put:    func(p, v string) error { wrote = append(wrote, p); return nil },
			})
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

// BFF token minting is gone, so an install no longer seeds or requires
// aep/task-signing-key: a store without it is not "wiped", which keeps an
// existing install's --reuse-secrets working either way.
func TestTaskSigningKey_NotRequired(t *testing.T) {
	if slices.Contains(requiredOpenBaoPaths(), "aep/task-signing-key") {
		t.Error("aep/task-signing-key must not be in requiredOpenBaoPaths")
	}
}

// aep-api does not talk to OpenBao, so an install neither seeds nor requires
// aep/openbao-token: a store without it is not "wiped", and a store that still
// has it fails nothing.
func TestOpenBaoToken_NotRequired(t *testing.T) {
	if slices.Contains(requiredOpenBaoPaths(), "aep/openbao-token") {
		t.Error("aep/openbao-token must not be in requiredOpenBaoPaths")
	}
}

// The idp.claims block is layered: the preset, then the config's keys. Each
// value says which layer it came from.
func TestPlatformIDPClaims_LayersPresetAndConfig(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("idp.claims.preset", "asgardeo")
	viper.Set("idp.claims.org.handle", "from_config")

	got, err := platformIDPClaims()
	if err != nil {
		t.Fatal(err)
	}
	want := idpclaims.Claims{OrgHandle: "from_config", OrgID: "org_id", OrgName: "org_name", CallerType: "aut", Machine: "APPLICATION", User: "APPLICATION_USER"}
	if got.Claims() != want {
		t.Fatalf("claims = %+v, want %+v", got.Claims(), want)
	}
	sources := map[string]idpclaims.Source{}
	for _, v := range got.Values {
		sources[v.Key] = v.Source
	}
	for key, src := range map[string]idpclaims.Source{
		"org.handle": idpclaims.SourceConfig,
		"org.id":     idpclaims.SourcePreset("asgardeo"), "org.name": idpclaims.SourcePreset("asgardeo"),
	} {
		if sources[key] != src {
			t.Errorf("%s came from %q, want %q", key, sources[key], src)
		}
	}
}

// A claim value is not taken from the environment: it would hold for one
// run, and the next update without the variable would send the stored value
// back to the chart. Any AEP_IDP_CLAIMS_* variable stops the command and is
// named, the preset's and a blank one included.
func TestPlatformIDPClaims_EnvironmentVariableRefused(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"AEP_IDP_CLAIMS_ORG_HANDLE", "org_handle"},
		{"AEP_IDP_CLAIMS_PRESET", "asgardeo"},
		{"AEP_IDP_CLAIMS_CALLER_TYPE_USER", " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(viper.Reset)
			t.Setenv(tc.name, tc.value)
			_, err := platformIDPClaims()
			if err == nil || !strings.Contains(err.Error(), tc.name) || !strings.Contains(err.Error(), "idp.claims") {
				t.Fatalf("err = %v, want one naming %s and idp.claims", err, tc.name)
			}
		})
	}
	// Another AEP_* variable is not a claim value.
	t.Setenv("AEP_IDP_ORG_HANDLE", "default")
	if _, err := platformIDPClaims(); err != nil {
		t.Fatalf("AEP_IDP_ORG_HANDLE must not be refused: %v", err)
	}
}

// A config that names no preset starts from the default one, ThunderID's.
func TestPlatformIDPClaims_AnUnsetPresetIsTheDefault(t *testing.T) {
	t.Cleanup(viper.Reset)
	got, err := platformIDPClaims()
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != idpclaims.DefaultPreset || got.Claims().OrgHandle != "ouHandle" {
		t.Fatalf("no preset: preset %q, org handle claim %q; want the default and ouHandle", got.Preset, got.Claims().OrgHandle)
	}
}

// Every claim value is passed to the chart, the empty optional ones too: the
// chart's own defaults are one IdP's, and a pair left out would keep it. No
// kind is passed.
func TestPlatformIDPOverrides_SetsEveryClaimValue(t *testing.T) {
	args := platformIDPOverrides(testPlatformIDP)
	pairs := map[string]bool{}
	for i := 0; i+1 < len(args); i += 2 {
		pairs[args[i]+" "+args[i+1]] = true
	}
	for pair := range pairs {
		if strings.Contains(pair, "idp.kind") {
			t.Errorf("a kind is still passed to the chart: %s", pair)
		}
	}
	for _, want := range []string{
		"--set-string idp.claims.org.handle=ouHandle",
		"--set-string idp.claims.org.id=ouId",
		"--set-string idp.claims.org.name=ouName",
		"--set-string idp.claims.callerType.claim=grant_type",
		"--set-string idp.claims.callerType.machine=client_credentials",
		"--set-string idp.claims.callerType.user=",
	} {
		if !pairs[want] {
			t.Errorf("missing %q in %v", want, args)
		}
	}
}
