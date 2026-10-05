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

package observability

// client.go — the OpenChoreo Observer's log query.
//
// ONE endpoint serves both readers: `POST /api/v1/logs/query`, whose
// `searchScope` is either a WORKFLOW scope (a build's WorkflowRun) or a
// COMPONENT scope (namespace + project + environment, and optionally one
// component). A coding cycle is read by the second shape — see cycle_logs.go
// for how its paging works on an API with no cursor.
//
// This is telemetry, not a log system of record: the observer's retention is
// the dataplane's, not ours.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// queryPageLimit is the observer's own per-query cap (a larger limit is a 400),
// so a page this long is the "there may be another page" signal.
const queryPageLimit = 1000

// defaultLookback is how far back a build-log read starts.
const defaultLookback = 30 * 24 * time.Hour

// Client reads logs from the observability plane.
type Client interface {
	// GetBuildLogs reads one build's log. `since` narrows the window to entries
	// after that instant — the tail read behind the console's log cursor; a zero
	// `since` reads the whole retention window.
	GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string, since time.Time) (*gen.BuildLogs, error)

	// QueryCycleLogs reads one coding cycle's pod lines across the cycle's
	// window: every line the Component with q.ComponentUID wrote, each once, in
	// index order. It reads the Component scope while the Component exists
	// and the project scope after it is deleted (q.Component == "").
	QueryCycleLogs(ctx context.Context, q CycleLogQuery) ([]LogLine, error)
}

// CycleLogQuery scopes one cycle's read. Namespace is the OpenChoreo namespace
// (the org handle).
type CycleLogQuery struct {
	Namespace, Project, Environment string
	// Component is the SCOPED component name. Empty reads the project scope:
	// the only scope that still answers once the Component is deleted, since
	// the observer resolves a component NAME to its UID on every query.
	Component string
	// ComponentUID is required. Lines whose metadata.componentUid differs are
	// dropped: other Components on a project read, and a later Component that
	// reuses the name (a new UID) on either read.
	ComponentUID string
	// From and To bound the read and are sent as given (the observer allows
	// at most 30 days). Both are exclusive at the observer, to the second.
	From, To time.Time
}

// LogLine is one indexed pod line. Timestamp is the observer's, to the SECOND:
// it orders lines across seconds, never within one.
type LogLine struct {
	Timestamp    time.Time
	Log          string
	ComponentUID string
	PodName      string
}

type observabilityClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new observability client. baseURL is the observer's base
// URL (e.g. https://observer.obs.dp.example.com).
func NewClient(baseURL string) Client {
	return &observabilityClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// componentScope / workflowScope are the two shapes of the request's
// `searchScope`. They are separate structs because the observer REJECTS a body
// that mixes workflowRunName with component fields, and `omitempty` on one
// struct would be one forgotten field away from sending exactly that.
type componentScope struct {
	Namespace   string `json:"namespace"`
	Project     string `json:"project,omitempty"`
	Component   string `json:"component,omitempty"`
	Environment string `json:"environment,omitempty"`
}

type workflowScope struct {
	Namespace       string `json:"namespace"`
	WorkflowRunName string `json:"workflowRunName"`
}

type logsQueryRequest struct {
	SearchScope  any    `json:"searchScope"`
	StartTime    string `json:"startTime"`
	EndTime      string `json:"endTime"`
	Limit        int    `json:"limit,omitempty"`
	SortOrder    string `json:"sortOrder,omitempty"`
	SearchPhrase string `json:"searchPhrase,omitempty"`
}

type logsQueryEntry struct {
	Timestamp string `json:"timestamp"`
	Log       string `json:"log"`
	Level     string `json:"level"`
	// Metadata is present on component-scope entries only.
	Metadata *logsQueryMetadata `json:"metadata,omitempty"`
}

type logsQueryMetadata struct {
	ComponentUID string `json:"componentUid"`
	PodName      string `json:"podName"`
}

type logsQueryResponse struct {
	Logs []logsQueryEntry `json:"logs"`
	// Total is the observer's field name. `totalCount` is accepted too because
	// the OpenChoreo CLI's own client still spells it that way, and a body that
	// carries only the older name must not read as zero results.
	Total      *int `json:"total,omitempty"`
	TotalCount *int `json:"totalCount,omitempty"`
}

// total is the match count of the request's window, when the body carries one.
func (r *logsQueryResponse) total() (int, bool) {
	switch {
	case r.Total != nil:
		return *r.Total, true
	case r.TotalCount != nil:
		return *r.TotalCount, true
	}
	return 0, false
}

func (c *observabilityClient) GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string, since time.Time) (*gen.BuildLogs, error) {
	now := time.Now().UTC()
	start := now.Add(-defaultLookback)
	if !since.IsZero() && since.After(start) {
		start = since.UTC()
	}
	// A build IS a WorkflowRun, and the run name is unique within the namespace,
	// so the project and component are not part of its scope. They stay on the
	// signature because they are the caller's tenancy fence, checked before the
	// call ever reaches here.
	resp, err := c.query(ctx, logsQueryRequest{
		SearchScope: workflowScope{Namespace: orgName, WorkflowRunName: buildName},
		StartTime:   start.Format(time.RFC3339),
		EndTime:     now.Format(time.RFC3339),
		Limit:       queryPageLimit,
		SortOrder:   "asc",
	})
	if err != nil {
		return nil, err
	}

	logs := &gen.BuildLogs{Logs: []gen.BuildLogEntry{}}
	if total, ok := resp.total(); ok {
		logs.TotalCount = int64(total)
	}
	for _, e := range resp.Logs {
		entry := gen.BuildLogEntry{Log: e.Log, Level: e.Level}
		if ts, perr := time.Parse(time.RFC3339, e.Timestamp); perr == nil {
			entry.Timestamp = ts.UTC().Format(time.RFC3339)
		} else {
			entry.Timestamp = e.Timestamp
		}
		logs.Logs = append(logs.Logs, entry)
	}
	return logs, nil
}

// query issues one POST /api/v1/logs/query. The caller's bearer is forwarded:
// the observer authorizes on the OpenChoreo identity, so an archive read is
// scoped to whoever asked for it.
func (c *observabilityClient) query(ctx context.Context, body logsQueryRequest) (*logsQueryResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("observability: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/logs/query", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("observability: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := auth.GetAuthToken(ctx); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("observability: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("observability: unexpected status %d", resp.StatusCode)
	}
	var out logsQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("observability: decode response: %w", err)
	}
	return &out, nil
}
