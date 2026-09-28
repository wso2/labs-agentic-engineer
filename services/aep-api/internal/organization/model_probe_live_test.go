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

package organization

// LIVE tier — the real prober, through the real SSRF-guarded client, against
// Ollama Cloud, on both formats. Skipped unless AEP_LIVE_MODEL_PROBE=1 and
// OLLAMA_API_KEY are set; the key is read from the environment only, never
// logged, and never written anywhere.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

func TestLiveProbe_OllamaCloud(t *testing.T) {
	key := os.Getenv("OLLAMA_API_KEY")
	if os.Getenv("AEP_LIVE_MODEL_PROBE") != "1" || key == "" {
		t.Skip("set AEP_LIVE_MODEL_PROBE=1 and OLLAMA_API_KEY to probe Ollama Cloud")
	}
	probers := newModelProbers(defaultModelProbeClient())
	for _, tc := range []struct {
		format     modelconn.Format
		typed      string
		wantScheme modelconn.AuthScheme
	}{
		{modelconn.FormatOpenAICompatible, "https://ollama.com/v1", modelconn.AuthBearer},
		// The bare host the Anthropic SDKs would 405 on: normalised to /v1, and
		// Ollama's Anthropic endpoint takes the key only as Bearer (measured).
		{modelconn.FormatAnthropic, "https://ollama.com", modelconn.AuthBearer},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			d, _, err := draftConnection(nil, orgconfig.LLMPatch{Kind: tc.format, BaseURL: tc.typed, APIKey: key, Model: "gpt-oss:20b"})
			if err != nil {
				t.Fatalf("draft: %v", err)
			}
			if d.BaseURL != "https://ollama.com/v1" {
				t.Fatalf("base URL = %s, want https://ollama.com/v1", d.BaseURL)
			}
			res, err := probers.probe(context.Background(), probeTarget{
				Org: "live", Format: d.Format, BaseURL: d.BaseURL, Host: d.Host, Model: d.Model, Key: key,
			})
			if err != nil {
				msg := err.Error()
				if strings.Contains(msg, key) {
					msg = "[error text carried the key; redacted]"
				}
				t.Fatalf("probe: %s", msg)
			}
			t.Logf("%s: scheme=%s listed=%s contextWindow=%v outputLimit=%v imageInput=%s providerLimited=%v",
				tc.format, res.AuthScheme, res.ModelListed, deref(res.ContextWindow), deref(res.OutputLimit), res.ImageInput, res.ProviderLimited)
			if res.AuthScheme != tc.wantScheme || res.ModelListed != modelconn.Yes {
				t.Fatalf("scheme=%s listed=%s, want %s and yes", res.AuthScheme, res.ModelListed, tc.wantScheme)
			}
			if res.ContextWindow == nil || *res.ContextWindow <= 0 || res.ImageInput == modelconn.Unknown {
				t.Fatalf("Ollama enrichment did not land: window=%v image=%s", deref(res.ContextWindow), res.ImageInput)
			}
			// Ollama's listing is public, so a wrong key must still be refused.
			_, err = probers.probe(context.Background(), probeTarget{
				Org: "live", Format: d.Format, BaseURL: d.BaseURL, Host: d.Host, Model: d.Model, Key: "not-a-real-ollama-key-0000",
			})
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Code != "llm_key_rejected" {
				t.Fatalf("a wrong key: %v, want llm_key_rejected", err)
			}
		})
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
