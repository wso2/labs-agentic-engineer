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

// model_probe.go — is this model connection usable? One ModelProber per API
// format answers it at save time (and for Test connection): is the key good,
// is the model served, how does the key authenticate, and what the host says
// about the model (context window, image input).
//
// The probe lists models rather than spending a completion: on an endpoint
// whose listing is authenticated (Anthropic's own API), GET {baseURL}/models
// proves the key and names the model in one call. A listing can be public,
// though — Ollama's answers 200 with no key at all (measured) — so a listing
// that also answers an unauthenticated request proves nothing about the key,
// and the key is proved with one max_tokens=1 request to the chosen model, as
// on an endpoint with no listing.
//
// Every call goes through the netguard client with NO redirects: Go forwards
// `x-api-key` across a redirect, so a followed redirect would carry the org's
// key to another host. A redirect answer is llm_unreachable.
//
// The outcome is one of the `llm_*` refusal codes (a ValidationError, or an
// UpstreamError for the endpoint's own 5xx), or a ProbeResult. A 429 that
// looks like a rate limit proves the key — an org whose plan is spent can
// still save — and is reported as a warning.
package organization

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/netguard"
	"github.com/wso2/aep/aep-api/internal/platform/text"
)

// The limits a connection gets when its host states none: what OpenCode and
// the agents service need to compact and cap output on a model they do not
// know. Anthropic's own API keeps NULL (the runtimes know Claude's limits).
const (
	defaultContextWindow = 128_000
	defaultOutputLimit   = 64_000
)

// modelProbeTimeout bounds each probe call, connect and body alike.
const modelProbeTimeout = 15 * time.Second

// modelListBodyCap bounds a model listing; OpenRouter-sized catalogs fit.
const modelListBodyCap = 8 << 20

// probeTarget is the connection a probe checks, and its key.
type probeTarget struct {
	Org     string // for the logs only
	Format  modelconn.Format
	BaseURL string
	Host    string
	Model   string
	Key     string
}

// ProbeResult is what a passing probe learned. AuthScheme, ContextWindow,
// OutputLimit and ImageInput are stored on the connection row; ModelListed and
// ProviderLimited are reported to the card, never stored.
type ProbeResult struct {
	AuthScheme    modelconn.AuthScheme
	ModelListed   modelconn.Tristate
	ContextWindow *int
	OutputLimit   *int
	ImageInput    modelconn.Tristate
	// ProviderLimited: the endpoint answered with a rate limit, which proves
	// the key but means agents will wait on (or be blocked by) the provider.
	ProviderLimited bool
}

// ModelProber checks one API format's connection.
type ModelProber interface {
	Probe(ctx context.Context, t probeTarget) (ProbeResult, error)
}

// modelProbers picks the prober per format; all share one no-redirect client,
// which also makes the host-specific enrichment call.
type modelProbers struct {
	byFormat map[modelconn.Format]ModelProber
	h        *probeHTTP
}

// newModelProbers builds the probers on client, forcing its redirect policy to
// none whatever the caller set: a prober holds a key, so no caller may hand it
// a client that follows redirects.
func newModelProbers(client *http.Client) modelProbers {
	c := *client
	c.CheckRedirect = netguard.NoRedirects()
	h := &probeHTTP{client: &c}
	return modelProbers{h: h, byFormat: map[modelconn.Format]ModelProber{
		modelconn.FormatAnthropic:        anthropicProber(h),
		modelconn.FormatOpenAICompatible: openAICompatibleProber(h),
	}}
}

// defaultModelProbeClient is the production client: public addresses only.
func defaultModelProbeClient() *http.Client {
	return netguard.NewClient(modelProbeTimeout, netguard.NoRedirects())
}

func (p modelProbers) probe(ctx context.Context, t probeTarget) (ProbeResult, error) {
	prober, ok := p.byFormat[t.Format]
	if !ok {
		return ProbeResult{}, &ValidationError{Code: "llm_field_required", Message: fmt.Sprintf("format %q is not one this platform offers", t.Format)}
	}
	res, err := prober.Probe(ctx, t)
	if err != nil {
		return ProbeResult{}, err
	}
	p.enrichFromHost(ctx, t, &res)
	return res, nil
}

// --- the two formats ----------------------------------------------------------

// formatProber probes one API format: where it lists models, where it takes
// a request, how it presents the key, and whether a 401 on x-api-key earns a
// Bearer retry.
type formatProber struct {
	h        *probeHTTP
	listPath string
	sendPath string
	headers  func(scheme modelconn.AuthScheme, key string) http.Header
	// schemes are the auth schemes tried in order, the next only after a 401.
	schemes []modelconn.AuthScheme
}

// anthropicProber is the Anthropic Messages format. It presents the key as
// `x-api-key` first and, on a 401, once more as `Authorization: Bearer`:
// Ollama's Anthropic endpoint takes only Bearer (measured), Anthropic's own
// API only x-api-key. The scheme that worked is what the runtimes are told.
// limit=1000: Anthropic pages its listing at 20 by default.
func anthropicProber(h *probeHTTP) *formatProber {
	return &formatProber{
		h: h, listPath: "/models?limit=1000", sendPath: "/messages",
		headers: anthropicHeaders,
		schemes: []modelconn.AuthScheme{modelconn.AuthXAPIKey, modelconn.AuthBearer},
	}
}

// openAICompatibleProber is the OpenAI chat-completions format, which always
// presents the key as Bearer.
func openAICompatibleProber(h *probeHTTP) *formatProber {
	return &formatProber{
		h: h, listPath: "/models", sendPath: "/chat/completions",
		headers: func(scheme modelconn.AuthScheme, key string) http.Header {
			h := http.Header{}
			setAuth(h, scheme, key)
			return h
		},
		schemes: []modelconn.AuthScheme{modelconn.AuthBearer},
	}
}

func (p *formatProber) Probe(ctx context.Context, t probeTarget) (ProbeResult, error) {
	scheme, resp, err := p.authenticated(ctx, t, http.MethodGet, p.listPath, nil)
	if err != nil {
		return ProbeResult{}, err
	}
	switch resp.status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		// No listing: one request proves the key; the model stays unknown.
		scheme, resp, err := p.send(ctx, t)
		if err != nil {
			return ProbeResult{}, err
		}
		if resp.status == http.StatusOK || resp.status == http.StatusBadRequest {
			return ProbeResult{AuthScheme: scheme, ModelListed: modelconn.Unknown}, nil
		}
		return statusVerdict(t, scheme, resp)
	case http.StatusOK:
	default:
		return statusVerdict(t, scheme, resp)
	}

	listed := modelListed(resp.body, t.Model)
	public, err := p.listingIsPublic(ctx, t)
	if err != nil || !public {
		return ProbeResult{AuthScheme: scheme, ModelListed: listed}, err
	}
	// A public listing proved nothing: one request proves the key and finds
	// the scheme it takes. 404 here is the endpoint (whose listing just
	// answered) saying it does not serve this model, after it authenticated
	// the key — the unlisted-model warning, not a refusal.
	scheme, resp, err = p.send(ctx, t)
	if err != nil {
		return ProbeResult{}, err
	}
	switch resp.status {
	case http.StatusOK, http.StatusBadRequest, http.StatusNotFound:
		return ProbeResult{AuthScheme: scheme, ModelListed: listed}, nil
	}
	return statusVerdict(t, scheme, resp)
}

// send is the one max_tokens=1 request to the chosen model.
func (p *formatProber) send(ctx context.Context, t probeTarget) (modelconn.AuthScheme, probeResponse, error) {
	body := fmt.Appendf(nil, `{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, t.Model)
	return p.authenticated(ctx, t, http.MethodPost, p.sendPath, body)
}

// authenticated makes one call with each scheme in turn, moving on only after
// a 401, and returns the scheme of the answer it kept.
func (p *formatProber) authenticated(ctx context.Context, t probeTarget, method, path string, body []byte) (modelconn.AuthScheme, probeResponse, error) {
	var (
		scheme modelconn.AuthScheme
		resp   probeResponse
		err    error
	)
	for _, scheme = range p.schemes {
		resp, err = p.h.do(ctx, t, method, t.BaseURL+path, body, p.headers(scheme, t.Key))
		if err != nil || resp.status != http.StatusUnauthorized {
			break
		}
	}
	return scheme, resp, err
}

// listingIsPublic asks the listing again with no credential at all: an
// endpoint that still answers 200 lists models for anyone, so its listing
// proves nothing about the key.
func (p *formatProber) listingIsPublic(ctx context.Context, t probeTarget) (bool, error) {
	headers := p.headers(p.schemes[0], "")
	headers.Del("x-api-key")
	headers.Del("Authorization")
	resp, err := p.h.do(ctx, t, http.MethodGet, t.BaseURL+p.listPath, nil, headers)
	if err != nil {
		return false, err
	}
	return resp.status == http.StatusOK, nil
}

func anthropicHeaders(scheme modelconn.AuthScheme, key string) http.Header {
	h := http.Header{}
	h.Set("anthropic-version", "2023-06-01")
	setAuth(h, scheme, key)
	return h
}

func setAuth(h http.Header, scheme modelconn.AuthScheme, key string) {
	if scheme == modelconn.AuthBearer {
		h.Set("Authorization", "Bearer "+key)
		return
	}
	h.Set("x-api-key", key)
}

// --- verdicts -----------------------------------------------------------------

// statusVerdict maps every answer that is not a success.
func statusVerdict(t probeTarget, scheme modelconn.AuthScheme, resp probeResponse) (ProbeResult, error) {
	switch {
	case resp.status >= 300 && resp.status < 400:
		return ProbeResult{}, &ValidationError{Code: "llm_unreachable", Message: fmt.Sprintf(
			"%s answered with a redirect (%d), which is never followed; enter the URL the endpoint is served at", t.Host, resp.status)}
	case resp.status == http.StatusUnauthorized || resp.status == http.StatusForbidden:
		return ProbeResult{}, &ValidationError{Code: "llm_key_rejected", Message: fmt.Sprintf(
			"%s rejected the key (%d)", t.Host, resp.status)}
	case resp.status == http.StatusTooManyRequests && rateLimitShaped(resp):
		return ProbeResult{AuthScheme: scheme, ModelListed: modelconn.Unknown, ProviderLimited: true}, nil
	case resp.status >= 500:
		return ProbeResult{}, &UpstreamError{Code: "llm_upstream_error", Message: fmt.Sprintf(
			"%s returned %d: %s", t.Host, resp.status, scrubbedExcerpt(resp.body, t.Key))}
	default:
		return ProbeResult{}, &ValidationError{Code: "llm_unexpected_status", Message: fmt.Sprintf(
			"%s returned %d: %s", t.Host, resp.status, scrubbedExcerpt(resp.body, t.Key))}
	}
}

// modelListed reads an OpenAI- or Anthropic-shaped listing ({data: [{id}]}).
// An Anthropic listing that says more pages exist and did not name the model
// cannot say "no".
func modelListed(body []byte, model string) modelconn.Tristate {
	var listing struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal(body, &listing); err != nil || listing.Data == nil {
		return modelconn.Unknown
	}
	for _, m := range listing.Data {
		if m.ID == model {
			return modelconn.Yes
		}
	}
	if listing.HasMore {
		return modelconn.Unknown
	}
	return modelconn.No
}

// rateLimitShaped tells a provider's rate limit from a WAF or edge refusal
// that also says 429: a retry-after, a rate-limit header, or an error body
// whose type names a rate limit.
func rateLimitShaped(resp probeResponse) bool {
	if len(limitHeaders(resp.header)) > 0 {
		return true
	}
	var body struct {
		Type  string `json:"type"`
		Code  any    `json:"code"`
		Error struct {
			Type string `json:"type"`
			Code any    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(resp.body, &body) != nil {
		return false
	}
	for _, v := range []any{body.Type, body.Code, body.Error.Type, body.Error.Code} {
		if s, ok := v.(string); ok && namesRateLimit(s) {
			return true
		}
	}
	return false
}

func namesRateLimit(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "rate") && strings.Contains(s, "limit")
}

// limitHeaders are the response headers that describe a limit, names and
// values, for the rate-limit check and the model_provider_429 line.
func limitHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for name, values := range h {
		n := strings.ToLower(name)
		if n == "retry-after" || n == "retry-after-ms" || strings.HasPrefix(n, "x-ratelimit-") || strings.HasPrefix(n, "ratelimit") {
			out[n] = strings.Join(values, ", ")
		}
	}
	return out
}

// --- host enrichment ------------------------------------------------------------

// hostFacts is what a host-specific call learned about the model.
type hostFacts struct {
	contextWindow int
	imageInput    modelconn.Tristate
}

// enrichFromHost fills the limits and image input a passing probe stores. A
// listing carries no context or capability fields on the hosts checked so far,
// so Ollama gets one extra call; Anthropic's own API keeps NULL limits (the
// runtimes know Claude) and reads images; every other host gets the defaults
// and "unknown". Enrichment never fails a save: a host that will not say keeps
// the defaults.
func (p modelProbers) enrichFromHost(ctx context.Context, t probeTarget, res *ProbeResult) {
	if t.Host == modelconn.AnthropicHost {
		res.ImageInput = modelconn.Yes
		return
	}
	window, output := defaultContextWindow, defaultOutputLimit
	res.ContextWindow, res.OutputLimit, res.ImageInput = &window, &output, modelconn.Unknown
	if t.Host != modelconn.OllamaHost {
		return
	}
	if facts, ok := p.h.ollamaShow(ctx, t, res.AuthScheme); ok {
		if facts.contextWindow > 0 {
			window := facts.contextWindow
			res.ContextWindow = &window
		}
		res.ImageInput = facts.imageInput
	}
}

// ollamaShow asks Ollama about the model: POST {origin}/api/show gives its
// capabilities (`vision`) and `<arch>.context_length` (measured on cloud
// models). The call goes to the connection's own host with its own key, so the
// key never follows another host.
func (h *probeHTTP) ollamaShow(ctx context.Context, t probeTarget, scheme modelconn.AuthScheme) (hostFacts, bool) {
	u, err := url.Parse(t.BaseURL)
	if err != nil {
		return hostFacts{}, false
	}
	headers := http.Header{}
	setAuth(headers, scheme, t.Key)
	resp, err := h.do(ctx, t, http.MethodPost, u.Scheme+"://"+u.Host+"/api/show", fmt.Appendf(nil, `{"model":%q}`, t.Model), headers)
	if err != nil || resp.status != http.StatusOK {
		return hostFacts{}, false
	}
	var show struct {
		Capabilities []string       `json:"capabilities"`
		ModelInfo    map[string]any `json:"model_info"`
	}
	if json.Unmarshal(resp.body, &show) != nil {
		return hostFacts{}, false
	}
	facts := hostFacts{imageInput: modelconn.No}
	for _, c := range show.Capabilities {
		if c == "vision" {
			facts.imageInput = modelconn.Yes
		}
	}
	for k, v := range show.ModelInfo {
		if n, ok := v.(float64); ok && strings.HasSuffix(k, ".context_length") && n > 0 {
			facts.contextWindow = int(n)
		}
	}
	return facts, true
}

// --- transport ------------------------------------------------------------------

// probeHTTP makes the probe's calls and turns transport failures into refusal
// codes. Every 429 it sees is logged as model_provider_429.
type probeHTTP struct{ client *http.Client }

type probeResponse struct {
	status int
	header http.Header
	body   []byte
}

func (h *probeHTTP) do(ctx context.Context, t probeTarget, method, target string, body []byte, headers http.Header) (probeResponse, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return probeResponse{}, &ValidationError{Code: "llm_base_url_invalid", Message: fmt.Sprintf("the base URL does not form a request: %v", err)}
	}
	req.Header = headers.Clone()
	req.Header.Set("accept", "application/json")
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return probeResponse{}, transportRefusal(t.Host, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, modelListBodyCap))
	out := probeResponse{status: resp.StatusCode, header: resp.Header, body: raw}
	if resp.StatusCode == http.StatusTooManyRequests {
		logProvider429(ctx, t, out)
	}
	return out, nil
}

// transportRefusal names a call that got no answer. It echoes the host as
// typed and never a resolved address: naming what an internal name resolves
// to would make the refusal a blind-SSRF oracle.
func transportRefusal(host string, err error) error {
	if errors.Is(err, netguard.ErrNonPublicAddress) {
		return &ValidationError{Code: "llm_host_refused", Message: fmt.Sprintf(
			"%s resolves to a private, loopback or cluster address; only public https endpoints can be connected", host)}
	}
	reason := "the connection failed"
	var dnsErr *net.DNSError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		reason = "the host name does not resolve"
	case errors.As(err, &netErr) && netErr.Timeout():
		reason = "the request timed out"
	}
	return &ValidationError{Code: "llm_unreachable", Message: fmt.Sprintf("could not reach %s: %s", host, reason)}
}

// logProvider429 writes the one structured line every 429 gets, so what a
// provider's limit looks like is learned from the logs. The body has the key's
// literal removed, then is capped.
func logProvider429(ctx context.Context, t probeTarget, resp probeResponse) {
	verdict := "unexpected_status"
	if rateLimitShaped(resp) {
		verdict = "provider_limit"
	}
	slog.WarnContext(ctx, "model_provider_429",
		"source", "probe",
		"org", t.Org,
		"host", t.Host,
		"format", t.Format,
		"model", t.Model,
		"status", resp.status,
		"limitHeaders", limitHeaders(resp.header),
		"body", text.Truncate(scrubKey(string(resp.body), t.Key), 300),
		"verdict", verdict)
}

// scrubbedExcerpt is an answer's body as an error quotes it: the key removed
// first, then cut, so a key straddling the cut leaves no prefix behind.
func scrubbedExcerpt(body []byte, key string) string {
	return truncateForError([]byte(scrubKey(string(body), key)))
}

// scrubKey removes the key's literal from text headed for a log or an error.
func scrubKey(text, key string) string {
	if key == "" {
		return text
	}
	return strings.ReplaceAll(text, key, "[redacted]")
}
