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
	"bytes"
	"slices"
	"strings"
	"testing"
)

// setMinimalEnv sets the variables Load requires and clears AE_STUDIO_*.
func setMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV_FILE_PATH", "")
	t.Setenv("PLATFORM_API_SERVICE_BASE_URL", "http://platform-api.invalid")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("JWKS_URL", "http://idp.invalid/jwks")
	for _, k := range []string{
		"AE_STUDIO_IMAGE_DESIGN_AGENT", "AE_STUDIO_IMAGE_COLLAB", "AE_STUDIO_IMAGE_STUDIO_TOOLS",
		"AE_STUDIO_GATEWAY_HOST", "AE_STUDIO_PUBLIC_SCHEME", "AE_STUDIO_PUBLIC_PORT_SUFFIX",
		"AE_STUDIO_LISTENER_NAME", "AE_STUDIO_CONSOLE_ORIGINS", "AE_STUDIO_IDP_ISSUER",
		"AE_STUDIO_IDP_JWKS_URL", "AE_STUDIO_IDP_TOKEN_URL", "AE_STUDIO_IDP_USER_AUDIENCES",
		"AE_STUDIO_AEP_API_BASE_URL", "AE_STUDIO_INTERNAL_CLIENT_ID", "AE_STUDIO_INTERNAL_CLIENT_SECRET",
		"AE_STUDIO_RUNTIME_CLASS_NAME", "AE_STUDIO_CILIUM", "AE_STUDIO_EXTRA_EGRESS",
		"AE_STUDIO_STORAGE_SIZE_LIMIT", "AE_STUDIO_STORAGE_EPHEMERAL_REQUEST",
		"AE_STUDIO_STORAGE_BUDGET_BYTES", "AE_STUDIO_CPU_REQUEST_DESIGN_AGENT", "AE_STUDIO_CPU_REQUEST_COLLAB", "AE_STUDIO_CPU_REQUEST_STUDIO_TOOLS", "AE_STUDIO_PULL_SECRET_KEY", "AE_STUDIO_PULL_SECRET_PROPERTY",
		"AE_STUDIO_WEBHOOK_RELAY_SEED", "AE_STUDIO_WEBHOOK_RELAY_IMAGE", "AE_STUDIO_WEBHOOK_RELAY_ENABLED",
		"AE_STUDIO_JEV_API_KEY", "AE_STUDIO_JEV_URL",
		"CREDENTIAL_ENCRYPTION_KEY",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_WithoutAEStudio(t *testing.T) {
	setMinimalEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("boot must not need AE_STUDIO_*: %v", err)
	}
	m := cfg.AEStudio.Missing()
	for _, k := range []string{"AE_STUDIO_IMAGE_DESIGN_AGENT", "AE_STUDIO_GATEWAY_HOST", "AE_STUDIO_INTERNAL_CLIENT_ID", "AE_STUDIO_CONSOLE_ORIGINS"} {
		if !slices.Contains(m, k) {
			t.Errorf("Missing() lacks %s: %v", k, m)
		}
	}
	if slices.Contains(m, "AE_STUDIO_INTERNAL_CLIENT_SECRET") {
		t.Error("the AE-only secret is not needed to Ensure (it is aep-api's own client, used from phase 2)")
	}
	if string(cfg.AEStudio.ExtraEgress) != "[]" || cfg.AEStudio.ListenerName != "https" || cfg.AEStudio.Storage.SizeLimit != "3Gi" || cfg.AEStudio.Storage.EphemeralRequest != "1Gi" {
		t.Errorf("defaults wrong: %+v", cfg.AEStudio)
	}
}

func TestLoad_AEStudioParsed(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("AE_STUDIO_CONSOLE_ORIGINS", "http://console.ae.localhost:8080, http://localhost:8090")
	t.Setenv("AE_STUDIO_EXTRA_EGRESS", `[{"ports":[{"port":8090}]}]`)
	t.Setenv("AE_STUDIO_CILIUM", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AEStudio.ConsoleOrigins) != 2 || !cfg.AEStudio.Cilium || cfg.AEStudio.Storage.BudgetBytes != 2147483648 || cfg.AEStudio.PublicScheme != "https" {
		t.Fatalf("%+v", cfg.AEStudio)
	}
	t.Setenv("AE_STUDIO_EXTRA_EGRESS", `{not json`)
	if _, err := Load(); err == nil {
		t.Fatal("malformed AE_STUDIO_EXTRA_EGRESS must fail boot (it is a deployment typo, not an absent feature)")
	}
}

// The Jev key is optional and needs nothing else: unset is off, set is on.
func TestLoad_Jev(t *testing.T) {
	setMinimalEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AEStudio.Jev.On() {
		t.Fatal("no key must be off")
	}
	t.Setenv("AE_STUDIO_JEV_API_KEY", "jev-secret")
	t.Setenv("AE_STUDIO_JEV_URL", "https://jev.example/v1/systemone")
	if cfg, _ = Load(); !cfg.AEStudio.Jev.On() || cfg.AEStudio.Jev.URL != "https://jev.example/v1/systemone" {
		t.Fatalf("on=%v url=%q", cfg.AEStudio.Jev.On(), cfg.AEStudio.Jev.URL)
	}
}

// Relay keys used by the relay tests: testRelayCredKey is bytes 0x00..0x1f,
// testRelayCredKey2 bytes 0x20..0x3f, testRelayBadKey is not base64.
const (
	testRelayCredKey  = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	testRelayCredKey2 = "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="
	testRelayBadKey   = "not base64!"
	testRelaySeed     = "seed-hex-text"
	testRelayImage    = "ghcr.io/chmouel/gosmee@sha256:abc"
)

// How AE_STUDIO_WEBHOOK_RELAY_ENABLED, AE_STUDIO_WEBHOOK_RELAY_SEED,
// AE_STUDIO_WEBHOOK_RELAY_IMAGE and CREDENTIAL_ENCRYPTION_KEY resolve to the
// relay key. "" is unset. Unset flag = the seed decides; "true" = on, the key
// derived from the credential key when no seed is set; "false" = off.
func TestLoad_WebhookRelay(t *testing.T) {
	derived := mustHex(t, goldenRelayKeyHex)
	type want int
	const (
		off want = iota
		explicit
		derivedKey
		bootErr
	)
	for _, tc := range []struct {
		name, flag, seed, image, key string
		want                         want
		imageMissing                 bool
		errHas                       []string
	}{
		{name: "T1 nothing set is off", key: testRelayCredKey, want: off},
		{name: "T2 image alone is inert", image: testRelayImage, key: testRelayCredKey, want: off},
		{name: "T3 false is off", flag: "false", image: testRelayImage, key: testRelayCredKey, want: off},
		{name: "T4 false beats a seed", flag: "false", seed: testRelaySeed, image: testRelayImage, key: testRelayCredKey, want: off},
		{name: "T5 seed alone is today's relay", seed: testRelaySeed, image: testRelayImage, key: testRelayCredKey, want: explicit},
		{name: "T6 seed without image is not configured", seed: testRelaySeed, key: testRelayCredKey, want: explicit, imageMissing: true},
		{name: "T7 true derives from the credential key", flag: "true", image: testRelayImage, key: testRelayCredKey, want: derivedKey},
		{name: "T8 true without image is not configured", flag: "true", key: testRelayCredKey, want: derivedKey, imageMissing: true},
		{name: "T9 an explicit seed wins over derivation", flag: "true", seed: testRelaySeed, image: testRelayImage, key: testRelayCredKey, want: explicit},
		{name: "T10 true with the placeholder key fails boot", flag: "true", image: testRelayImage, want: bootErr,
			errHas: []string{"AE_STUDIO_WEBHOOK_RELAY_ENABLED", "CREDENTIAL_ENCRYPTION_KEY"}},
		{name: "T11 an explicit seed needs no real key", flag: "true", seed: testRelaySeed, image: testRelayImage, want: explicit},
		{name: "T12 a malformed key keeps its own error", flag: "true", image: testRelayImage, key: testRelayBadKey, want: bootErr,
			errHas: []string{"CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setMinimalEnv(t)
			t.Setenv("AE_STUDIO_WEBHOOK_RELAY_ENABLED", tc.flag)
			t.Setenv("AE_STUDIO_WEBHOOK_RELAY_SEED", tc.seed)
			t.Setenv("AE_STUDIO_WEBHOOK_RELAY_IMAGE", tc.image)
			t.Setenv("CREDENTIAL_ENCRYPTION_KEY", tc.key)
			cfg, err := Load()
			if tc.want == bootErr {
				if err == nil {
					t.Fatal("want a boot error")
				}
				for _, s := range tc.errHas {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q lacks %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := cfg.AEStudio.WebhookRelaySeed
			switch tc.want {
			case off:
				if len(got) != 0 {
					t.Errorf("relay key set (%d bytes), want off", len(got))
				}
			case explicit:
				if !bytes.Equal(got, []byte(tc.seed)) {
					t.Errorf("relay key is not the explicit seed's text")
				}
			case derivedKey:
				if !bytes.Equal(got, derived) {
					t.Errorf("relay key is not HKDF(credential key)")
				}
			}
			if m := slices.Contains(cfg.AEStudio.Missing(), "AE_STUDIO_WEBHOOK_RELAY_IMAGE"); m != tc.imageMissing {
				t.Errorf("image in Missing() = %v, want %v", m, tc.imageMissing)
			}
		})
	}
}

// Only "", "true" and "false" are a flag; anything else is a deployment typo
// and fails boot naming the key, never echoing key material.
func TestLoad_WebhookRelayFlagMalformedFailsBoot(t *testing.T) {
	for _, v := range []string{"True", "1", "yes", " true"} {
		t.Run(v, func(t *testing.T) {
			setMinimalEnv(t)
			t.Setenv("AE_STUDIO_WEBHOOK_RELAY_ENABLED", v)
			t.Setenv("AE_STUDIO_WEBHOOK_RELAY_IMAGE", testRelayImage)
			t.Setenv("CREDENTIAL_ENCRYPTION_KEY", testRelayCredKey)
			_, err := Load()
			if err == nil {
				t.Fatal("want a boot error")
			}
			msg := err.Error()
			if !strings.Contains(msg, `AE_STUDIO_WEBHOOK_RELAY_ENABLED must be "true" or "false"`) {
				t.Errorf("error %q does not name the key", msg)
			}
			if strings.Contains(msg, testRelayCredKey) || strings.Contains(msg, goldenRelayKeyHex) {
				t.Errorf("error %q carries key material", msg)
			}
		})
	}
}

// The relay key is a function of the environment alone, so every restart
// (and every replica) lands each org on the same channel.
func TestLoad_WebhookRelayDerivedKeyStable(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("AE_STUDIO_WEBHOOK_RELAY_ENABLED", "true")
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", testRelayCredKey)
	a, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.AEStudio.WebhookRelaySeed) == 0 || !bytes.Equal(a.AEStudio.WebhookRelaySeed, b.AEStudio.WebhookRelaySeed) {
		t.Fatal("two loads of the same env gave different relay keys")
	}
}

func TestLoad_AEStudioCPURequests(t *testing.T) {
	setMinimalEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AEStudio.CPURequest; got.DesignAgent != "100m" || got.Collab != "50m" || got.StudioTools != "100m" {
		t.Fatalf("defaults = %+v, want 100m/50m/100m", got)
	}
	t.Setenv("AE_STUDIO_CPU_REQUEST_DESIGN_AGENT", "0.25")
	t.Setenv("AE_STUDIO_CPU_REQUEST_COLLAB", "10m")
	t.Setenv("AE_STUDIO_CPU_REQUEST_STUDIO_TOOLS", "1000m")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.AEStudio.CPURequest; got.DesignAgent != "250m" || got.Collab != "10m" || got.StudioTools != "1" {
		t.Fatalf("set = %+v, want 250m/10m/1", got)
	}
}

func TestLoad_AEStudioCPURequestsMalformedFailBootNamingTheKey(t *testing.T) {
	const secretish = "bogus-cpu-value-xyz"
	for _, key := range []string{"AE_STUDIO_CPU_REQUEST_DESIGN_AGENT", "AE_STUDIO_CPU_REQUEST_COLLAB", "AE_STUDIO_CPU_REQUEST_STUDIO_TOOLS"} {
		for _, bad := range []string{secretish, "0", "-1", "2001m", "3", "2066035336255469781"} {
			setMinimalEnv(t)
			t.Setenv(key, bad)
			_, err := Load()
			if err == nil {
				t.Fatalf("%s=%q must fail Load", key, bad)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%q: error must name the key: %v", key, bad, err)
			}
			if len(bad) > 5 && strings.Contains(err.Error(), bad) {
				t.Fatalf("%s: error must not echo the value: %v", key, err)
			}
		}
	}
}

// The org's pod reads the same IdP's tokens as aep-api, so the kind it is
// told (AE_STUDIO_IDP_KIND) follows PLATFORM_IDP_KIND when it is not set,
// and a different explicit kind is a boot error: the pod would otherwise
// read every token by the wrong rules and refuse it.
func TestLoad_AEStudioIDPKindFollowsThePlatformKind(t *testing.T) {
	asgardeo := func(t *testing.T) {
		t.Helper()
		setMinimalEnv(t)
		t.Setenv("PLATFORM_IDP_KIND", "asgardeo")
		t.Setenv("PLATFORM_IDP_ORG_HANDLE", "acmecorp")
		t.Setenv("PLATFORM_IDP_ORG_NAMESPACE", "default")
		t.Setenv("PLATFORM_IDP_ORG_PUBLISHER_SECRET", "p")
		t.Setenv("PLATFORM_IDP_ORG_STUDIO_SECRET", "s")
	}

	t.Run("unset follows the platform kind", func(t *testing.T) {
		asgardeo(t)
		t.Setenv("AE_STUDIO_IDP_KIND", "")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.AEStudio.IDP.Kind != "asgardeo" {
			t.Fatalf("AEStudio.IDP.Kind = %q, want asgardeo", cfg.AEStudio.IDP.Kind)
		}
	})
	t.Run("the same kind set explicitly", func(t *testing.T) {
		asgardeo(t)
		t.Setenv("AE_STUDIO_IDP_KIND", "asgardeo")
		if _, err := Load(); err != nil {
			t.Fatalf("Load: %v", err)
		}
	})
	t.Run("a different kind is refused", func(t *testing.T) {
		asgardeo(t)
		t.Setenv("AE_STUDIO_IDP_KIND", "thunder")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AE_STUDIO_IDP_KIND") || !strings.Contains(err.Error(), "PLATFORM_IDP_KIND") {
			t.Fatalf("Load = %v, want an error naming both settings", err)
		}
	})
	t.Run("a thunder install sets neither", func(t *testing.T) {
		setMinimalEnv(t)
		t.Setenv("PLATFORM_IDP_KIND", "")
		t.Setenv("AE_STUDIO_IDP_KIND", "")
		cfg, err := Load()
		if err != nil || cfg.AEStudio.IDP.Kind != "thunder" {
			t.Fatalf("Load = %q, %v; want thunder", cfg.AEStudio.IDP.Kind, err)
		}
	})
}
