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
// COMPONENT scope (a deployed component's pods — which is what an ephemeral
// coding-agent cycle is). The observer offers no cursor and no offset, so
// paging is done by moving the time window past the last entry returned.
//
// This is telemetry, not a log system of record: the observer answers only
// while the component/run it indexes still exists, and its retention is the
// dataplane's, not ours.

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

// queryPageLimit is the observer's own per-query cap. Asking for more is not
// honoured, so it doubles as the "there is another page" signal.
const queryPageLimit = 1000

// maxQueryPages bounds a windowed read. At the page limit that is 20k lines —
// far past anything a console renders, and the stop that keeps a misbehaving
// backend from turning one poll into an unbounded loop.
const maxQueryPages = 20

// defaultLookback is how far back a cursor-less read starts.
const defaultLookback = 30 * 24 * time.Hour

// Client reads logs from the observability plane.
type Client interface {
	// GetBuildLogs reads one build's log. `since` narrows the window to entries
	// after that instant — the tail read behind the console's log cursor; a zero
	// `since` reads the whole retention window.
	GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string, since time.Time) (*gen.BuildLogs, error)

	// QueryComponentLogs reads a component's archived pod logs across a time
	// window, following the observer's windowed paging. It is what serves a
	// finished coding cycle whose pod is gone but whose Component is retained.
	QueryComponentLogs(ctx context.Context, q ComponentLogQuery) ([]LogLine, error)
}

// ComponentLogQuery scopes an archive read. Namespace is the OpenChoreo
// namespace (the org handle), Component the SCOPED component name.
type ComponentLogQuery struct {
	Namespace   string
	Project     string
	Component   string
	Environment string
	From        time.Time
	To          time.Time
}

// LogLine is one archived line.
type LogLine struct {
	Timestamp time.Time
	Log       string
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
	SearchScope any    `json:"searchScope"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	Limit       int    `json:"limit,omitempty"`
	SortOrder   string `json:"sortOrder,omitempty"`
}

type logsQueryEntry struct {
	Timestamp string `json:"timestamp"`
	Log       string `json:"log"`
	Level     string `json:"level"`
}

type logsQueryResponse struct {
	Logs []logsQueryEntry `json:"logs"`
	// Total is the observer's field name. `totalCount` is accepted too because
	// the OpenChoreo CLI's own client still spells it that way, and a body that
	// carries only the older name must not read as zero results.
	Total      *int `json:"total,omitempty"`
	TotalCount *int `json:"totalCount,omitempty"`
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
	if resp.Total != nil {
		logs.TotalCount = int64(*resp.Total)
	} else if resp.TotalCount != nil {
		logs.TotalCount = int64(*resp.TotalCount)
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

func (c *observabilityClient) QueryComponentLogs(ctx context.Context, q ComponentLogQuery) ([]LogLine, error) {
	scope := componentScope{
		Namespace:   q.Namespace,
		Project:     q.Project,
		Component:   q.Component,
		Environment: q.Environment,
	}
	from, to := q.From.UTC(), q.To.UTC()
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-defaultLookback)
	}

	var out []LogLine
	for page := 0; page < maxQueryPages; page++ {
		resp, err := c.query(ctx, logsQueryRequest{
			SearchScope: scope,
			StartTime:   from.Format(time.RFC3339),
			EndTime:     to.Format(time.RFC3339),
			Limit:       queryPageLimit,
			SortOrder:   "asc",
		})
		if err != nil {
			return nil, err
		}
		var last time.Time
		for _, e := range resp.Logs {
			line := LogLine{Log: e.Log}
			if ts, perr := time.Parse(time.RFC3339, e.Timestamp); perr == nil {
				line.Timestamp = ts.UTC()
				last = line.Timestamp
			}
			out = append(out, line)
		}
		if len(resp.Logs) < queryPageLimit {
			return out, nil
		}
		if last.IsZero() || !last.Before(to) {
			// A full page whose entries carry no usable timestamp cannot be
			// advanced past; stop rather than re-request the same window.
			return out, nil
		}
		from = last.Add(time.Millisecond)
	}
	return out, nil
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
