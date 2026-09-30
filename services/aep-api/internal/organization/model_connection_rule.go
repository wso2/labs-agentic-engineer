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

// model_connection_rule.go — what an `llm` patch leaves, decided purely: the
// patch merged field by field over the stored connection, the format's
// defaults on first connect, and the refusals that need no network (the URL's
// shape, the key's length, a new host without a key). The save and Test
// connection both start here; agents_rule.go folds the result into the card.
package organization

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// minKeyLength is the shortest connection key accepted: the runner's log
// scrubber drops literals under 12 characters (MIN_LITERAL_LEN), so a shorter
// key would reach a build log unredacted.
const minKeyLength = 12

// connectionDraft is the connection a patch leaves, before the probe adds what
// only the endpoint can tell (auth scheme, limits, image input).
type connectionDraft struct {
	Format  modelconn.Format
	BaseURL string
	Host    string
	Model   string
	// Key is the key sent with the patch; "" reuses the stored key, which is
	// only ever allowed on the stored connection's origin (keyOrigin).
	Key string
}

// connection is the draft as CapabilitiesOf reads it: capabilities key on
// format and host, which the draft already fixes.
func (d connectionDraft) connection() modelconn.Connection {
	return modelconn.Connection{Format: d.Format, BaseURL: d.BaseURL, Host: d.Host, Model: d.Model}
}

// draftConnection merges w over stored (nil = no connection yet). changed is
// false when w leaves the stored connection exactly as it is, so a save need
// neither probe nor write it. Every refusal is a ValidationError with an
// `llm_*` (or Anthropic key-shape) code.
func draftConnection(stored *OrgModelConnection, w orgconfig.LLMPatch) (connectionDraft, bool, error) {
	kind := modelconn.Format(strings.TrimSpace(string(w.Kind)))
	rawURL := strings.TrimSpace(w.BaseURL)
	key := strings.TrimSpace(w.APIKey)
	model := strings.TrimSpace(w.Model)

	var d connectionDraft
	if stored == nil {
		// Clause 1: first connect needs a format and a key, plus a URL when the
		// format has no default; the model and a defaultable URL are filled in.
		if kind == "" || key == "" {
			return connectionDraft{}, false, errFieldRequired("a first connection needs a format (kind) and an apiKey")
		}
		opt, ok := modelconn.FormatOptionOf(kind)
		if !ok {
			return connectionDraft{}, false, errFieldRequired(fmt.Sprintf("format %q is not one this platform offers", kind))
		}
		if rawURL == "" {
			if opt.DefaultBaseURL == "" {
				return connectionDraft{}, false, errFieldRequired(fmt.Sprintf("the %s format has no default URL; a baseURL is required", kind))
			}
			rawURL = opt.DefaultBaseURL
		}
		if model == "" {
			model = opt.DefaultModel
		}
		d = connectionDraft{Format: kind, Model: model}
	} else {
		d = connectionDraft{Format: stored.Format, BaseURL: stored.BaseURL, Host: stored.Host, Model: stored.Model}
		if kind != "" {
			d.Format = kind
		}
		if model != "" {
			d.Model = model
		}
		if rawURL == "" {
			rawURL = stored.BaseURL
		}
	}

	baseURL, host, err := normalizeBaseURL(d.Format, rawURL)
	if err != nil {
		return connectionDraft{}, false, err
	}
	d.BaseURL, d.Host, d.Key = baseURL, host, key

	// Clause 2: a change of origin (host or port) needs a key. A format change
	// on the same origin keeps it: Ollama serves both formats on one host with
	// one key.
	if stored != nil && d.Key == "" {
		if err := requireKeyForNewOrigin(stored.BaseURL, d.BaseURL); err != nil {
			return connectionDraft{}, false, err
		}
	}
	if d.Key != "" {
		if err := checkKeyShape(d.Host, d.Key); err != nil {
			return connectionDraft{}, false, err
		}
	}
	changed := stored == nil || d.Key != "" ||
		d.Format != stored.Format || d.BaseURL != stored.BaseURL || d.Model != stored.Model
	return d, changed, nil
}

// normalizeBaseURL checks what the org typed before any DNS lookup and returns
// the URL as it is stored, and its host. https only, with no userinfo, query or
// fragment; a trailing slash and an explicit :443 go. An Anthropic-format URL with no path gains
// `/v1`: the SDKs append `/messages` to it, so the bare host would answer 405.
func normalizeBaseURL(format modelconn.Format, raw string) (string, string, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return "", "", errBaseURL(fmt.Sprintf("%q is not a URL", raw))
	case u.Scheme != "https":
		return "", "", errBaseURL("the base URL must be https")
	case u.User != nil:
		return "", "", errBaseURL("the base URL must not carry credentials; put the key in apiKey")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return "", "", errBaseURL("the base URL must not carry a query or a fragment")
	case u.Opaque != "" || u.Hostname() == "":
		return "", "", errBaseURL("the base URL needs a host")
	}
	host := strings.ToLower(u.Hostname())
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path == "" && format == modelconn.FormatAnthropic {
		path = "/v1"
	}
	return "https://" + authorityOf(u) + path, host, nil
}

// requireKeyForNewOrigin refuses a save that would send a stored key to
// another origin (ADR-0038 §5): moving from storedURL to baseURL needs the key
// for the new origin in the same save. Called only for a save that sent none.
func requireKeyForNewOrigin(storedURL, baseURL string) error {
	from, to := keyOrigin(storedURL), keyOrigin(baseURL)
	if from == to {
		return nil
	}
	return &ValidationError{
		Code: "llm_key_required_for_new_host",
		Message: fmt.Sprintf("the connection moves from %s to %s; send the key for %s in the same save "+
			"(a stored key is never sent to another host)", from, to, to),
	}
}

// keyOrigin is where a stored base URL sends its key: host and port, the
// https default left implied, so https://x and https://x:443 are one origin
// and https://x:8443 another. The key follows the origin, not the host alone:
// another port can be another server.
func keyOrigin(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	return authorityOf(u)
}

// authorityOf is u's lower-cased host with its port, dropping https's default.
func authorityOf(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && port != "443" {
		return net.JoinHostPort(host, port)
	}
	return host
}

// checkKeyShape refuses a key that could never work, before a probe is spent
// on it. The `sk-ant-` shape and the subscription-token refusal apply only on
// Anthropic's own API: third-party keys have no fixed shape.
func checkKeyShape(host, key string) error {
	if len(key) < minKeyLength {
		return &ValidationError{
			Code:    "llm_key_too_short",
			Message: fmt.Sprintf("the key is %d characters; keys under %d cannot be redacted from build logs", len(key), minKeyLength),
		}
	}
	if host != modelconn.AnthropicHost {
		return nil
	}
	if !looksLikeAnthropicKey(key) {
		return &ValidationError{Code: "anthropic_key_invalid", Message: "value does not look like an Anthropic API key (expected prefix 'sk-ant-')"}
	}
	if AnthropicCredentialKindOf(key) == AnthropicCredentialOAuth {
		return &ValidationError{
			Code: "anthropic_oauth_token_coding_only",
			Message: "a Claude subscription token can only bill the coding agent; " +
				"the connection's key must be a Console API key (sk-ant-api…)",
		}
	}
	return nil
}

// runtimesFor are the runtimes among available that run format: OpenCode runs
// both formats; Claude Code speaks only the Anthropic one.
func runtimesFor(format modelconn.Format, available []orgconfig.AgentRuntime) []orgconfig.AgentRuntime {
	out := []orgconfig.AgentRuntime{}
	for _, r := range available {
		if runtimeRuns(r, format) {
			out = append(out, r)
		}
	}
	return out
}

func runtimeRuns(r orgconfig.AgentRuntime, format modelconn.Format) bool {
	return r != orgconfig.AgentRuntimeClaudeCode || modelconn.CapabilitiesOf(modelconn.Connection{Format: format}).ClaudeCode
}

// errFormatHasNoRuntime refuses a format no runtime on this installation runs.
func errFormatHasNoRuntime(format modelconn.Format, available []orgconfig.AgentRuntime) *ValidationError {
	return &ValidationError{
		Code: "llm_format_has_no_runtime",
		Message: fmt.Sprintf("no runtime on this installation runs the %s format (available: %s); "+
			"OpenAI-compatible connections need the OpenCode runner image", format, runtimeNames(available)),
	}
}

func errFieldRequired(msg string) *ValidationError {
	return &ValidationError{Code: "llm_field_required", Message: msg}
}

func errBaseURL(msg string) *ValidationError {
	return &ValidationError{Code: "llm_base_url_invalid", Message: msg}
}
