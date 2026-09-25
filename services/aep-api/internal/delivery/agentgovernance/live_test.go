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

package agentgovernance

import (
	"context"
	"os"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// TestLiveGovernance drives the real Agent Manager on a local cluster.
//
// Skipped unless AEP_LIVE_AMP=1, because it writes: it registers an agent,
// binds it to the org's provider and issues a key. It exists because every
// request shape in the agentmanager client was read out of Agent Manager's
// console rather than a published spec, and unit tests can only prove we send
// what we MEANT to send. This proves Agent Manager accepts it.
//
// Run against a local stack with:
//
//	AEP_LIVE_AMP=1 AMP_BASE_URL=http://api.amp.localhost:8080/api/v1 \
//	  AMP_TOKEN_URL=http://thunder.openchoreo.localhost:8080/oauth2/token \
//	  go test ./internal/delivery/agentgovernance/ -run TestLiveGovernance -v
func TestLiveGovernance(t *testing.T) {
	if os.Getenv("AEP_LIVE_AMP") != "1" {
		t.Skip("set AEP_LIVE_AMP=1 to run against a live Agent Manager")
	}
	base := envOr("AMP_BASE_URL", "http://api.amp.localhost:8080/api/v1")
	tokenURL := envOr("AMP_TOKEN_URL", "http://thunder.openchoreo.localhost:8080/oauth2/token")
	orgKey := os.Getenv("AEP_LIVE_ORG_ANTHROPIC_KEY")
	if orgKey == "" {
		t.Fatal("AEP_LIVE_ORG_ANTHROPIC_KEY is required: the provider holds the org's key")
	}
	agentName := envOr("AEP_LIVE_AGENT", "aep-live-check-agent")

	keys := &liveKeyStore{}
	g := New(Deps{
		AMP: liveFactory{cfg: agentmanager.Config{
			TokenURL:     tokenURL,
			ClientID:     envOr("AMP_CLIENT_ID", "amp-publisher-aep"),
			ClientSecret: envOr("AMP_CLIENT_SECRET", "amp-publisher-aep-secret"),
			Resource:     "urn:wso2:amp",
		}},
		Keys:     keys,
		Bindings: liveBinding{base: base},
		OrgKeys:  liveOrgKey{key: orgKey},
	})

	out, err := g.GovernAgent(context.Background(), delivery.GovernAgentInput{
		OrgID:       envOr("AEP_LIVE_ORG", "default"),
		ProjectID:   envOr("AEP_LIVE_PROJECT", "default"),
		Component:   agentName,
		Environment: envOr("AEP_LIVE_ENV", "default"),
	})
	if err != nil {
		t.Fatalf("GovernAgent against live Agent Manager: %v", err)
	}
	if out.Skipped {
		t.Fatalf("governance skipped: %s", out.Reason)
	}
	if keys.stored == "" {
		t.Fatal("no model key was stored; the agent would deploy with no credential")
	}
	t.Logf("agent %q registered; key issued (%d chars); endpoint %s", agentName, len(keys.stored), keys.url)

	// AEP_LIVE_KEY_OUT lets an operator verify the last hop by hand — the key
	// authenticates against that proxy and nothing else, so a call through it is
	// the only proof that the governed path actually carries traffic.
	if out := os.Getenv("AEP_LIVE_KEY_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(keys.stored+"\n"+keys.url+"\n"), 0o600); err != nil {
			t.Fatalf("write key material: %v", err)
		}
		t.Logf("key + proxy URL written to %s", out)
	}
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

type liveFactory struct{ cfg agentmanager.Config }

func (f liveFactory) For(baseURL string) agentmanager.Client {
	c := f.cfg
	c.BaseURL = baseURL
	return agentmanager.New(c)
}

type liveBinding struct{ base string }

func (b liveBinding) GetAIGatewayBinding(context.Context, string, string) (openchoreo.AIGatewayBinding, error) {
	return openchoreo.AIGatewayBinding{
		Endpoint:  envOr("AEP_LIVE_GATEWAY_ENDPOINT", "http://ai-gateway.amp.localhost:8084"),
		AdminURL:  b.base,
		GatewayID: envOr("AEP_LIVE_GATEWAY_ID", ""),
	}, nil
}

type liveOrgKey struct{ key string }

func (k liveOrgKey) AnthropicKeyValue(context.Context, string) (string, error) { return k.key, nil }

// liveKeyStore keeps the issued key in memory: this test proves Agent Manager's
// half, not SM-API's, and writing a real secret would need a cluster identity
// the test does not have.
type liveKeyStore struct{ stored, url, tracingToken string }

func (s *liveKeyStore) StoredAMPModelKey(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (s *liveKeyStore) WriteAMPModelKey(_ context.Context, _, _, _, apiKey, proxyURL string) (string, string, error) {
	s.stored, s.url = apiKey, proxyURL
	return "amp-model-live", "api-key", nil
}

func (s *liveKeyStore) WriteAMPTracingToken(_ context.Context, _, _, _, token string) error {
	s.tracingToken = token
	return nil
}
