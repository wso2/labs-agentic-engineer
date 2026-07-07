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

package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
)

const (
	internalDesignComponentsPath = "/internal/v1/orchestration/design/components"
	internalGateCheckPath        = "/internal/v1/orchestration/gate-check"
	internalDispatchTaskPath     = "/internal/v1/orchestration/tasks/dispatch"
	internalDeployTaskPath       = "/internal/v1/orchestration/tasks/deploy"
	internalAutoMergePath        = "/internal/v1/orchestration/tasks/auto-merge"
)

// HTTPClient is the concrete activity adapter for aep-api's internal
// orchestration surface. It keeps the worker pure flow-state: all integration
// side effects remain inside aep-api.
type HTTPClient struct {
	baseURL string
	bearer  string
	client  *http.Client
}

// NewHTTPClient builds an aep-api internal client. A nil httpClient gets a
// bounded default so activities never inherit net/http's no-timeout client.
func NewHTTPClient(baseURL, bearer string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		bearer:  bearer,
		client:  httpClient,
	}
}

var (
	_ DesignReader   = (*HTTPClient)(nil)
	_ GateChecker    = (*HTTPClient)(nil)
	_ TaskDispatcher = (*HTTPClient)(nil)
	_ PRMerger       = (*HTTPClient)(nil)
)

// Components reads the approved design components and dependency edges from
// aep-api.
func (c *HTTPClient) Components(ctx context.Context, org, project string) ([]ComponentSpec, error) {
	var out struct {
		Components []ComponentSpec `json:"components"`
	}
	if err := c.post(ctx, internalDesignComponentsPath, struct {
		Org     string `json:"org"`
		Project string `json:"project"`
	}{Org: org, Project: project}, &out); err != nil {
		return nil, err
	}
	return out.Components, nil
}

// RunChecks delegates an auto gate check to aep-api.
func (c *HTTPClient) RunChecks(ctx context.Context, in types.GateChecksInput) (types.GateChecksResult, error) {
	var out types.GateChecksResult
	if err := c.post(ctx, internalGateCheckPath, in, &out); err != nil {
		return types.GateChecksResult{}, err
	}
	return out, nil
}

// EnsureOrgWorkspace is a no-op client-side. The dispatch endpoint owns the
// namespace ResourceQuota/LimitRange ensure next to the preserved dispatch code.
func (c *HTTPClient) EnsureOrgWorkspace(context.Context, string) error { return nil }

// DispatchTask asks aep-api to create/read the execution row and run the
// preserved CodingExecutor dispatch path.
func (c *HTTPClient) DispatchTask(ctx context.Context, in types.TaskLifecycleInput) error {
	return c.post(ctx, internalDispatchTaskPath, in, nil)
}

// DeployTask delegates post-build deployment to aep-api.
func (c *HTTPClient) DeployTask(ctx context.Context, in types.TaskLifecycleInput) error {
	return c.post(ctx, internalDeployTaskPath, in, nil)
}

// MergePR delegates auto code-review merge to aep-api.
func (c *HTTPClient) MergePR(ctx context.Context, in types.TaskLifecycleInput) error {
	return c.post(ctx, internalAutoMergePath, in, nil)
}

func (c *HTTPClient) post(ctx context.Context, path string, in, out any) error {
	if c == nil || c.baseURL == "" {
		return nil
	}
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("post %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
