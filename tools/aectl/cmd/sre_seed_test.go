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
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func writeSeedKeyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return p
}

// TestResolveSreModelSeed_NoFlags reports the unchanged, no-seed behaviour
// when neither --llm-api-key-file nor --llm-model is given.
func TestResolveSreModelSeed_NoFlags(t *testing.T) {
	seed, err := resolveSreModelSeed("", "", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("resolveSreModelSeed: %v", err)
	}
	if seed != nil {
		t.Fatalf("seed = %+v, want nil (no seed)", seed)
	}
}

// TestResolveSreModelSeed_PairRequired: the key-file and model flags must be
// given together, in either direction.
func TestResolveSreModelSeed_PairRequired(t *testing.T) {
	keyFile := writeSeedKeyFile(t, "sk-test")

	if _, err := resolveSreModelSeed(keyFile, "", "https://api.openai.com/v1"); err == nil {
		t.Fatal("want error when --llm-model is missing")
	} else if !strings.Contains(err.Error(), "--llm-api-key-file") || !strings.Contains(err.Error(), "--llm-model") {
		t.Errorf("error = %q, want it to name both flags", err)
	}

	if _, err := resolveSreModelSeed("", "gpt-5.4", "https://api.openai.com/v1"); err == nil {
		t.Fatal("want error when --llm-api-key-file is missing")
	}
}

// TestResolveSreModelSeed_EmptyFileRejected: a key file that is empty (or
// whitespace-only) after trimming must be rejected rather than seeding an
// empty key.
func TestResolveSreModelSeed_EmptyFileRejected(t *testing.T) {
	keyFile := writeSeedKeyFile(t, "   \n\t")
	if _, err := resolveSreModelSeed(keyFile, "gpt-5.4", "https://api.openai.com/v1"); err == nil {
		t.Fatal("want error for an empty key file")
	}
}

// TestResolveSreModelSeed_MissingFileRejected: a key-file path that does not
// exist must fail rather than silently seeding nothing.
func TestResolveSreModelSeed_MissingFileRejected(t *testing.T) {
	if _, err := resolveSreModelSeed(filepath.Join(t.TempDir(), "missing"), "gpt-5.4", "https://api.openai.com/v1"); err == nil {
		t.Fatal("want error for a missing key file")
	}
}

// TestResolveSreModelSeed_TrimsKey: the key is read from the file, trimmed
// of surrounding whitespace — never taken from a flag value.
func TestResolveSreModelSeed_TrimsKey(t *testing.T) {
	keyFile := writeSeedKeyFile(t, "  sk-abc123\n")
	seed, err := resolveSreModelSeed(keyFile, "gpt-5.4", "https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("resolveSreModelSeed: %v", err)
	}
	if seed == nil {
		t.Fatal("seed = nil, want a resolved seed")
	}
	if seed.APIKey != "sk-abc123" {
		t.Errorf("seed.APIKey = %q, want trimmed %q", seed.APIKey, "sk-abc123")
	}
	if seed.Model != "gpt-5.4" {
		t.Errorf("seed.Model = %q, want %q", seed.Model, "gpt-5.4")
	}
	if seed.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("seed.BaseURL = %q, want %q", seed.BaseURL, "https://api.openai.com/v1")
	}
}

// TestEnsureSREModelSeedSecret_CreatesThreeKeys: a fresh secret carries the
// three trimmed values under apiKey/model/baseURL.
func TestEnsureSREModelSeedSecret_CreatesThreeKeys(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	seed := sreModelSeed{APIKey: "sk-abc123", Model: "gpt-5.4", BaseURL: "https://api.openai.com/v1"}

	if err := ensureSREModelSeedSecret(ctx, client, "wso2-aep", seed); err != nil {
		t.Fatalf("ensureSREModelSeedSecret: %v", err)
	}
	sec, err := client.CoreV1().Secrets("wso2-aep").Get(ctx, sreModelSeedSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get %s secret: %v", sreModelSeedSecretName, err)
	}
	want := map[string]string{"apiKey": "sk-abc123", "model": "gpt-5.4", "baseURL": "https://api.openai.com/v1"}
	for k, v := range want {
		if got := string(sec.Data[k]); got != v {
			t.Errorf("data[%q] = %q, want %q", k, got, v)
		}
	}
}

// TestEnsureSREModelSeedSecret_UpdatesExisting: unlike ensureSREAgentSecret
// (create-only), a re-run with a changed --llm-* seed must overwrite the
// secret it created earlier — this is aectl's own secret, not one aep-api
// writes real values into.
func TestEnsureSREModelSeedSecret_UpdatesExisting(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	first := sreModelSeed{APIKey: "sk-old", Model: "gpt-4", BaseURL: "https://api.openai.com/v1"}
	if err := ensureSREModelSeedSecret(ctx, client, "wso2-aep", first); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	second := sreModelSeed{APIKey: "sk-new", Model: "gpt-5.4", BaseURL: "https://api.example.com/v1"}
	if err := ensureSREModelSeedSecret(ctx, client, "wso2-aep", second); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	sec, err := client.CoreV1().Secrets("wso2-aep").Get(ctx, sreModelSeedSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get %s secret: %v", sreModelSeedSecretName, err)
	}
	if got := string(sec.Data["apiKey"]); got != "sk-new" {
		t.Errorf("data[apiKey] = %q, want the second ensure's value %q", got, "sk-new")
	}
	if got := string(sec.Data["model"]); got != "gpt-5.4" {
		t.Errorf("data[model] = %q, want %q", got, "gpt-5.4")
	}
}

// TestSreAgentPlatformUpdateConfig_SeedSecretName: the platform update config
// carries sreAgent.seed.secretName only when a seed was actually written —
// an install run without --llm-* flags must not touch the value (and
// certainly not clear a previously-seeded org's secretName).
func TestSreAgentPlatformUpdateConfig_SeedSecretName(t *testing.T) {
	p := sreParams{AEPNamespace: "wso2-aep", Org: "default", ObsNamespace: "obs", RcaName: "sre-agent", MCPHostname: "aep-mcp.openchoreo.localhost"}

	withoutSeed := sreAgentPlatformUpdateConfig(p, "deployments/helm-charts/platform", "", "", "")
	for _, s := range withoutSeed.HelmSets {
		if strings.Contains(s, "sreAgent.seed.secretName") {
			t.Errorf("HelmSets = %v, must not set sreAgent.seed.secretName when not seeding", withoutSeed.HelmSets)
		}
	}

	withSeed := sreAgentPlatformUpdateConfig(p, "deployments/helm-charts/platform", "", sreModelSeedSecretName, "somehash")
	found := false
	for _, s := range withSeed.HelmSets {
		if s == "sreAgent.seed.secretName="+sreModelSeedSecretName {
			found = true
		}
	}
	if !found {
		t.Errorf("HelmSets = %v, want sreAgent.seed.secretName=%s", withSeed.HelmSets, sreModelSeedSecretName)
	}
}

// TestSreAgentPlatformUpdateConfig_SeedHash: the platform update config
// carries sreAgent.seed.hash only when this run is actually seeding (a
// non-empty seedSecretName) — an install run without --llm-* flags must
// leave both values untouched on the release, same as
// TestSreAgentPlatformUpdateConfig_SeedSecretName above.
func TestSreAgentPlatformUpdateConfig_SeedHash(t *testing.T) {
	p := sreParams{AEPNamespace: "wso2-aep", Org: "default", ObsNamespace: "obs", RcaName: "sre-agent", MCPHostname: "aep-mcp.openchoreo.localhost"}

	withoutSeed := sreAgentPlatformUpdateConfig(p, "deployments/helm-charts/platform", "", "", "")
	for _, s := range withoutSeed.HelmSets {
		if strings.Contains(s, "sreAgent.seed.hash") {
			t.Errorf("HelmSets = %v, must not set sreAgent.seed.hash when not seeding", withoutSeed.HelmSets)
		}
	}

	withSeed := sreAgentPlatformUpdateConfig(p, "deployments/helm-charts/platform", "", sreModelSeedSecretName, "abc123")
	found := false
	for _, s := range withSeed.HelmSets {
		if s == "sreAgent.seed.hash=abc123" {
			found = true
		}
	}
	if !found {
		t.Errorf("HelmSets = %v, want sreAgent.seed.hash=abc123", withSeed.HelmSets)
	}
}

// TestSreModelSeedHash_ChangesWithModelOrKey: the hash aectl computes for a
// seed (reused for both the seed-marker identity aep-api itself recomputes,
// see sre_model_seed.go, and the sreAgent.seed.hash set above) must change
// whenever the model or the key changes, and stay stable when neither does —
// otherwise a rotated key would roll aep-api's pods needlessly, or worse, an
// actually-changed seed wouldn't roll them at all.
func TestSreModelSeedHash_ChangesWithModelOrKey(t *testing.T) {
	base := sreModelSeed{APIKey: "sk-abc123", Model: "gpt-5.4", BaseURL: "https://api.openai.com/v1"}
	baseHash := sreModelSeedHash(base)

	if got := sreModelSeedHash(base); got != baseHash {
		t.Errorf("hash is not stable for identical seeds: %q != %q", got, baseHash)
	}

	changedModel := base
	changedModel.Model = "gpt-4"
	if got := sreModelSeedHash(changedModel); got == baseHash {
		t.Errorf("hash did not change when Model changed: %q", got)
	}

	changedKey := base
	changedKey.APIKey = "sk-rotated"
	if got := sreModelSeedHash(changedKey); got == baseHash {
		t.Errorf("hash did not change when APIKey changed: %q", got)
	}

	if baseHash == "" {
		t.Error("hash must not be empty")
	}
}
