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

// Package kubeobs is the Kubernetes API the SRE agent reconciler speaks to on
// the observability plane: it patches the AE-owned Secret, stamps the
// Deployment's pod-template hash annotation, scales it, and reads the
// Deployment and its pods back. Plain net/http over the API's REST paths, the
// same shape as clients/thunderapp: no client-go.
//
// A Secret request or response is never put into an error or a log: its body
// carries the SRE agent's model key and MCP token. An error names the method,
// the path and the status code only.
package kubeobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/httpx"
	"github.com/wso2/aep/aep-api/internal/clients/kubeauth"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/sreagent"
)

const (
	mergePatch = "application/merge-patch+json"
	// requestTimeout bounds one call; the reconciler retries on its next tick.
	requestTimeout = 15 * time.Second
	// maxMessage caps the apiserver message an error carries.
	maxMessage = 256
)

// Client speaks to one Kubernetes API.
type Client struct {
	baseURL string
	auth    kubeauth.Authorizer
	http    *http.Client
}

// New builds a Client over cfg. BaseURL is required. TokenFile is read on each
// request so a projected service-account token rotation is picked up; CAFile,
// when set, must be a readable PEM bundle.
func New(cfg config.KubeAPIConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("kubeobs: KubeAPIConfig.BaseURL is required")
	}
	tr, err := kubeauth.Transport(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("kubeobs: %w", err)
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		auth:    kubeauth.NewAuthorizer(cfg.BearerToken, cfg.TokenFile),
		http:    &http.Client{Transport: httpx.WrapTransport(tr), Timeout: requestTimeout},
	}, nil
}

// PatchSecretData merge-patches the Secret's data with data (base64 on the
// wire, as the API stores it). Keys not in data are left alone.
func (c *Client) PatchSecretData(ctx context.Context, ns, name string, data map[string][]byte) error {
	body := map[string]any{"data": data}
	return c.do(ctx, http.MethodPatch, "/api/v1/namespaces/"+ns+"/secrets/"+name, body, nil, true)
}

// PatchTemplateAnnotation sets one annotation on the Deployment's pod
// template, which rolls its pods.
func (c *Client) PatchTemplateAnnotation(ctx context.Context, ns, deploy, key, value string) error {
	body := map[string]any{"spec": map[string]any{"template": map[string]any{
		"metadata": map[string]any{"annotations": map[string]string{key: value}}}}}
	return c.do(ctx, http.MethodPatch, deploymentPath(ns, deploy), body, nil, false)
}

// Scale sets the Deployment's replicas through its scale subresource.
func (c *Client) Scale(ctx context.Context, ns, deploy string, replicas int32) error {
	body := map[string]any{"spec": map[string]int32{"replicas": replicas}}
	return c.do(ctx, http.MethodPatch, deploymentPath(ns, deploy)+"/scale", body, nil, false)
}

// Deployment reads the Deployment's rollout state and its pod selector
// (spec.selector.matchLabels).
func (c *Client) Deployment(ctx context.Context, ns, deploy string) (sreagent.DeploymentState, map[string]string, error) {
	var d deployment
	if err := c.do(ctx, http.MethodGet, deploymentPath(ns, deploy), nil, &d, false); err != nil {
		return sreagent.DeploymentState{}, nil, err
	}
	// An unset spec.replicas is the API's default of 1.
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	return sreagent.DeploymentState{
		Replicas:           replicas,
		UpdatedReplicas:    d.Status.UpdatedReplicas,
		AvailableReplicas:  d.Status.AvailableReplicas,
		Generation:         d.Metadata.Generation,
		ObservedGeneration: d.Status.ObservedGeneration,
		TemplateHash:       d.Spec.Template.Metadata.Annotations[sreagent.HashAnnotation],
	}, d.Spec.Selector.MatchLabels, nil
}

// Pods lists the pods selector matches in ns, each with the hash its template
// carried and why its first container is down, if it is. An empty selector is
// refused: it would match every pod in the namespace.
func (c *Client) Pods(ctx context.Context, ns string, selector map[string]string) ([]sreagent.PodState, error) {
	if len(selector) == 0 {
		return nil, fmt.Errorf("kubeobs: pods in %s: empty label selector", ns)
	}
	q := url.Values{}
	q.Set("labelSelector", labelSelector(selector))
	var list podList
	if err := c.do(ctx, http.MethodGet, "/api/v1/namespaces/"+ns+"/pods?"+q.Encode(), nil, &list, false); err != nil {
		return nil, err
	}
	out := make([]sreagent.PodState, 0, len(list.Items))
	for _, p := range list.Items {
		st := sreagent.PodState{Hash: p.Metadata.Annotations[sreagent.HashAnnotation]}
		if cs := p.Status.ContainerStatuses; len(cs) > 0 {
			s := cs[0]
			if s.State.Waiting != nil {
				st.WaitingReason = s.State.Waiting.Reason
			}
			switch {
			case s.State.Terminated != nil:
				st.TerminatedReason = s.State.Terminated.Reason
				st.ExitCode = s.State.Terminated.ExitCode
			case s.LastState.Terminated != nil:
				st.ExitCode = s.LastState.Terminated.ExitCode
			}
		}
		out = append(out, st)
	}
	return out, nil
}

func deploymentPath(ns, deploy string) string {
	return "/apis/apps/v1/namespaces/" + ns + "/deployments/" + deploy
}

// labelSelector renders matchLabels as k=v pairs, sorted so the query is
// stable.
func labelSelector(m map[string]string) string {
	pairs := make([]string, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

// do sends one request, a merge patch when in is set, and decodes a 2xx body
// into out when out is set. sensitive keeps the apiserver's message out of the
// error: a Secret call's response may echo what was sent.
func (c *Client) do(ctx context.Context, method, path string, in, out any, sensitive bool) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("kubeobs: %s %s: encode: %w", method, path, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("kubeobs: %s %s: build request: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", mergePatch)
	}
	auth, err := c.auth.Header()
	if err != nil {
		return fmt.Errorf("kubeobs: %w", err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kubeobs: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("kubeobs: %s %s: read response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &HTTPError{Method: method, Path: path, StatusCode: resp.StatusCode}
		if !sensitive {
			e.Message = statusMessage(raw)
		}
		return e
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("kubeobs: %s %s: decode: %w", method, path, err)
	}
	return nil
}

// HTTPError is a non-2xx answer from the Kubernetes API. Message is the
// apiserver's Status message, and always empty for a Secret call.
type HTTPError struct {
	Method, Path string
	StatusCode   int
	Message      string
}

func (e *HTTPError) Error() string {
	s := fmt.Sprintf("kubeobs: %s %s → %d", e.Method, e.Path, e.StatusCode)
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

// statusMessage pulls the message out of a metav1.Status body, capped.
func statusMessage(raw []byte) string {
	var st struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &st) != nil {
		return ""
	}
	if len(st.Message) > maxMessage {
		return st.Message[:maxMessage]
	}
	return st.Message
}

type deployment struct {
	Metadata struct {
		Generation int64 `json:"generation"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int32 `json:"replicas"`
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
		Template struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration int64 `json:"observedGeneration"`
		UpdatedReplicas    int32 `json:"updatedReplicas"`
		AvailableReplicas  int32 `json:"availableReplicas"`
	} `json:"status"`
}

type podList struct {
	Items []struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Status struct {
			ContainerStatuses []struct {
				State     containerState `json:"state"`
				LastState containerState `json:"lastState"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

type containerState struct {
	Waiting *struct {
		Reason string `json:"reason"`
	} `json:"waiting"`
	Terminated *struct {
		Reason   string `json:"reason"`
		ExitCode int32  `json:"exitCode"`
	} `json:"terminated"`
}
