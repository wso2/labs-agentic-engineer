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

package modelconn

import "testing"

func TestCapabilitiesOf(t *testing.T) {
	cases := []struct {
		name string
		conn Connection
		want Capabilities
	}{
		{
			name: "anthropic@api.anthropic.com",
			conn: Connection{Format: FormatAnthropic, Host: AnthropicHost, ImageInput: Unknown},
			want: Capabilities{
				ClaudeCode: true, ClaudeSubscription: true, PromptCache: true, GeneratedAgents: true, NativePDF: true,
				WebSearch: WebSearchAnthropicServerTool, ImageInput: Yes,
			},
		},
		{
			name: "anthropic@ollama.com",
			conn: Connection{Format: FormatAnthropic, Host: OllamaHost, ImageInput: No},
			want: Capabilities{
				ClaudeCode: true, PromptCache: true, GeneratedAgents: true,
				WebSearch: WebSearchOllamaAPI, ImageInput: No,
			},
		},
		{
			name: "openai-compatible@ollama.com",
			conn: Connection{Format: FormatOpenAICompatible, Host: OllamaHost, ImageInput: Yes},
			want: Capabilities{GeneratedAgents: true, WebSearch: WebSearchOllamaAPI, ImageInput: Yes},
		},
		{
			name: "openai-compatible@openrouter.ai",
			conn: Connection{Format: FormatOpenAICompatible, Host: "openrouter.ai", ImageInput: Unknown},
			want: Capabilities{GeneratedAgents: true, WebSearch: WebSearchNone, ImageInput: Unknown},
		},
		{
			// Anthropic's host alone is not Anthropic's API: an OpenAI-compatible
			// call to it gets no first-party features.
			name: "openai-compatible@api.anthropic.com",
			conn: Connection{Format: FormatOpenAICompatible, Host: AnthropicHost, ImageInput: Unknown},
			want: Capabilities{GeneratedAgents: true, WebSearch: WebSearchNone, ImageInput: Unknown},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CapabilitiesOf(tc.conn); got != tc.want {
				t.Fatalf("CapabilitiesOf(%s)\n got %+v\nwant %+v", tc.name, got, tc.want)
			}
		})
	}
}

// Every format the card offers has a default model, and only a format bound to
// one host has a default URL.
func TestFormats(t *testing.T) {
	seen := map[Format]bool{}
	for _, f := range Formats {
		if seen[f.Format] {
			t.Fatalf("format %s listed twice", f.Format)
		}
		seen[f.Format] = true
		if f.DefaultModel == "" {
			t.Errorf("format %s has no default model", f.Format)
		}
	}
	if !seen[FormatAnthropic] || !seen[FormatOpenAICompatible] {
		t.Fatalf("Formats = %+v, want both formats", Formats)
	}
	if Formats[0].DefaultBaseURL != "https://api.anthropic.com/v1" {
		t.Errorf("anthropic default URL = %q", Formats[0].DefaultBaseURL)
	}
	if Formats[1].DefaultBaseURL != "" {
		t.Errorf("openai-compatible must have no default URL, got %q", Formats[1].DefaultBaseURL)
	}
}
