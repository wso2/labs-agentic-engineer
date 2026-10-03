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

package aestudio

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// The AE_MODEL_CONNECTION bytes are pinned: the design agent's
// connectionFromEnv parses them, and they are hashed into the pod's pinned
// parameters, so any byte change re-pins and rolls every org's pod.
func TestDesired_ModelConnectionBytesArePinned(t *testing.T) {
	window, limit := 128000, 8192
	for name, c := range map[string]struct {
		conn modelconn.Connection
		want string
	}{
		"anthropic": {
			anthropicConn("claude-x"),
			`{"format":"anthropic","baseURL":"https://api.anthropic.com/v1","authScheme":"x-api-key","capabilities":{"claudeCode":true,"claudeSubscription":true,"promptCache":true,"generatedAgents":true,"nativePdf":true,"webSearch":"anthropic-server-tool","imageInput":"yes"},"model":"claude-x"}`,
		},
		"openai-compatible with limits": {
			modelconn.Connection{Format: modelconn.FormatOpenAICompatible, BaseURL: "https://ollama.com/v1", Host: modelconn.OllamaHost,
				Model: "gpt-oss:120b", AuthScheme: modelconn.AuthBearer, ContextWindow: &window, OutputLimit: &limit, ImageInput: modelconn.No},
			`{"format":"openai-compatible","baseURL":"https://ollama.com/v1","authScheme":"bearer","contextWindow":128000,"outputLimit":8192,"capabilities":{"claudeCode":false,"claudeSubscription":false,"promptCache":false,"generatedAgents":true,"nativePdf":false,"webSearch":"ollama-api","imageInput":"no"},"model":"gpt-oss:120b"}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t).withAllRefs().withConnection(c.conn)
			d, err := f.svc.desired(ctx, "default")
			if err != nil {
				t.Fatal(err)
			}
			if d.Params.ModelConnection != c.want {
				t.Fatalf("AE_MODEL_CONNECTION bytes changed:\n got %s\nwant %s", d.Params.ModelConnection, c.want)
			}
		})
	}
}
