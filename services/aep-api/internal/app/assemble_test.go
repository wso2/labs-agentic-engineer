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

// The assembly-test tier: proves app.Assemble(cfg, Fake(), Seam{}) builds the real
// service graph in-process, with zero I/O, in milliseconds. This is what makes
// the composition root's "the harness can build the same graph with faked deps"
// promise true — Build's ~1,000 lines were previously exercised by nothing but a
// live process boot. These tests pin, per config-gated mode: the graph assembles
// without error, the watcher slice is registered at the right count, and the
// Degradations() matrix reports exactly which optional capabilities are off.
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/config"
)

// baseCfg is the minimal config that Assemble accepts. The OpenChoreo clients
// use a must-construct pattern that panics on an empty BaseURL — so the structurally-
// required (non-degradable) fields are set to dummy non-empty values. Everything
// OPTIONAL is left at its zero value, so the graph assembles in its maximally-
// degraded mode. Assemble never calls config.Validate, so the required-at-boot
// fields (JWKSURL, TaskTokenSigningKey) are irrelevant here.
func baseCfg() config.Config {
	c := config.Config{}
	c.PlatformAPI.BaseURL = "http://openchoreo.test"
	return c
}

// stubSecretsProvider is an injected Provider for assembly tests.
// ManagesSecretReferences=true so Assemble does not need a live OC adapter.
type stubSecretsProvider struct{}

func (stubSecretsProvider) NewClient(*secretmanagersvc.StoreConfig) (secretmanagersvc.SecretsClient, error) {
	return stubSecretsClient{}, nil
}
func (stubSecretsProvider) ValidateConfig(*secretmanagersvc.StoreConfig) error { return nil }
func (stubSecretsProvider) Capabilities() secretmanagersvc.StoreCapabilities {
	return secretmanagersvc.StoreCapabilityWriteOnly
}
func (stubSecretsProvider) ManagesSecretReferences() bool { return true }

type stubSecretsClient struct{}

func (stubSecretsClient) PushSecret(context.Context, secretmanagersvc.SecretLocation, []byte, *secretmanagersvc.SecretMetadata) (string, error) {
	return "", nil
}
func (stubSecretsClient) PatchSecret(context.Context, secretmanagersvc.SecretLocation, []byte, *secretmanagersvc.SecretMetadata) (string, error) {
	return "", nil
}
func (stubSecretsClient) DeleteSecret(context.Context, secretmanagersvc.SecretLocation, *secretmanagersvc.SecretMetadata) error {
	return nil
}
func (stubSecretsClient) GetSecret(context.Context, secretmanagersvc.SecretLocation) (*secretmanagersvc.SecretInfo, error) {
	return nil, nil
}
func (stubSecretsClient) GetSecretWithValue(context.Context, secretmanagersvc.SecretLocation) ([]byte, error) {
	return nil, nil
}
func (stubSecretsClient) Close(context.Context) error { return nil }

func TestAssemble_MinimalConfigBuildsTheGraph(t *testing.T) {
	app, err := Assemble(baseCfg(), Fake(), Seam{})
	if err != nil {
		t.Fatalf("Assemble(minimal, Fake()) = %v, want nil", err)
	}
	if app.Handler == nil {
		t.Fatal("assembled app has a nil Handler")
	}
	if len(app.Watchers) != 9 {
		t.Fatalf("minimal watcher count = %d, want 9 (the unconditional watchers; reaper omitted with Fake nil Workspace)", len(app.Watchers))
	}
	for i, w := range app.Watchers {
		if w == nil {
			t.Fatalf("watcher %d is nil", i)
		}
	}
}

// The edge's SRE gate and the auto-RCA switch read the one verifier Assemble
// builds from SRE_HANDOFF_TOKEN/SRE_HANDOFF_ORG (Review Focus 3). Through the
// assembled handler: configured, the bearer clears the sre/ gate and reaches
// the handler; unconfigured, the same bearer is refused. The report carries an
// unknown classification so the handler answers 400 from ops.NewReport before
// it would touch Fake()'s nil database.
func TestAssemble_SREHandoffVerifierShared(t *testing.T) {
	status := func(t *testing.T, cfg config.Config) int {
		t.Helper()
		app, err := Assemble(cfg, Fake(), Seam{})
		if err != nil {
			t.Fatalf("Assemble = %v", err)
		}
		report := `{"project":"p","title":"t","summary":"s","diagnosis":"d","classification":"vibes"}`
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/sre/rca-reports", strings.NewReader(report))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer s3cr3t")
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, req)
		return w.Code
	}
	t.Run("configured", func(t *testing.T) {
		cfg := baseCfg()
		cfg.SREHandoffToken, cfg.SREHandoffOrg = "s3cr3t", "acme"
		if got := status(t, cfg); got != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 from the handler (the bearer past the sre/ gate)", got)
		}
	})
	t.Run("unconfigured", func(t *testing.T) {
		if got := status(t, baseCfg()); got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})
}

// TestAssemble_WatcherRegistration pins the one remaining conditional watcher:
// the run-supervisor worker rides on TEMPORAL_HOSTPORT. The base is 9 — Fake()
// omits the disk reaper (nil Workspace); the webhook replayer, the event plane's
// reconcile AND build sweeps, the OpenChoreo pod-truth watcher and the model key
// rename are all unconditional.
func TestAssemble_WatcherRegistration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
		want   int
	}{
		{"base", func(*config.Config) {}, 9},
		{"+temporal adds the run worker", func(c *config.Config) {
			c.Temporal.HostPort = "temporal:7233"
		}, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg()
			tt.mutate(&cfg)
			app, err := Assemble(cfg, Fake(), Seam{})
			if err != nil {
				t.Fatalf("Assemble = %v", err)
			}
			if len(app.Watchers) != tt.want {
				t.Fatalf("watcher count = %d, want %d", len(app.Watchers), tt.want)
			}
		})
	}
}

func hasCapability(degs []Degradation, capability string) bool {
	for _, d := range degs {
		if d.Capability == capability {
			return true
		}
	}
	return false
}

// TestAssemble_Degradations walks the config-gated degraded modes off the
// assembled app's Degradations() report — the enumerable matrix that replaces a
// capability/Profile abstraction.
func TestAssemble_Degradations(t *testing.T) {
	t.Run("minimal config reports the full degraded set", func(t *testing.T) {
		app, err := Assemble(baseCfg(), Fake(), Seam{})
		if err != nil {
			t.Fatalf("Assemble = %v", err)
		}
		degs := app.Degradations()
		// Every optional capability is off, including no working coding-dispatch
		// path (AGENT_RUNNER_IMAGE unset and no secrets provider).
		for _, want := range []string{
			"m2m-service-auth", "build-logs", "secrets-delivery",
			"idp-mutations",
			"coding-dispatch-oc", "run-temporal", "ae-studio-tools",
		} {
			if !hasCapability(degs, want) {
				t.Errorf("minimal config: expected degradation %q, missing from %+v", want, degs)
			}
		}
	})

	t.Run("a secrets provider clears the OC dispatch degradation", func(t *testing.T) {
		app, err := Assemble(baseCfg(), Fake(), Seam{SecretsProvider: stubSecretsProvider{}})
		if err != nil {
			t.Fatalf("Assemble = %v", err)
		}
		degs := app.Degradations()
		for _, gone := range []string{"secrets-delivery", "coding-dispatch-oc"} {
			if hasCapability(degs, gone) {
				t.Errorf("with a secrets provider: %q should NOT be degraded, got %+v", gone, degs)
			}
		}
	})

	t.Run("temporal enabled clears the devflow degradation", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Temporal.HostPort = "temporal:7233"
		app, err := Assemble(cfg, Fake(), Seam{})
		if err != nil {
			t.Fatalf("Assemble = %v", err)
		}
		if hasCapability(app.Degradations(), "run-temporal") {
			t.Errorf("with TEMPORAL_HOSTPORT set, run-temporal must not be degraded")
		}
	})

	t.Run("the AE-only client clears the ae-studio-tools degradation", func(t *testing.T) {
		cfg := baseCfg()
		cfg.AEStudio.IDP.TokenURL = "http://thunder/oauth2/token"
		cfg.AEStudio.InternalClientID = "ae-studio-internal-client"
		cfg.AEStudio.InternalClientSecret = "test-secret"
		app, err := Assemble(cfg, Fake(), Seam{})
		if err != nil {
			t.Fatalf("Assemble = %v", err)
		}
		if hasCapability(app.Degradations(), "ae-studio-tools") {
			t.Errorf("with the AE-only client configured, ae-studio-tools must not be degraded")
		}
	})

	t.Run("an observer URL clears the archive degradation", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Observability.BaseURL = "http://observer"
		app, err := Assemble(cfg, Fake(), Seam{})
		if err != nil {
			t.Fatalf("Assemble = %v", err)
		}
		degs := app.Degradations()
		for _, gone := range []string{"build-logs", "cycle-log-archive"} {
			if hasCapability(degs, gone) {
				t.Errorf("with OBSERVER_URL set, %q must not be degraded, got %+v", gone, degs)
			}
		}
	})
}

// ResourceLabels are stamped on every OpenChoreo write, so a label the API
// server would reject must fail the boot rather than every write after it.
func TestAssemble_RefusesInvalidResourceLabels(t *testing.T) {
	_, err := Assemble(baseCfg(), Fake(), Seam{ResourceLabels: map[string]string{"cloud.wso2.com/product-name": "not a label value"}})
	if err == nil {
		t.Fatal("Assemble with an invalid resource label = nil error, want a refusal")
	}
	if _, err := Assemble(baseCfg(), Fake(), Seam{ResourceLabels: map[string]string{"cloud.wso2.com/product-name": "app-factory"}}); err != nil {
		t.Fatalf("Assemble with a valid resource label = %v, want nil", err)
	}
}
