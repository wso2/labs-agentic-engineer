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

// alerts.go — the OpenChoreo Observer's alert query, read with aep-api's own
// service identity rather than a forwarded user token: the SRE handoff asks
// it whether an alert really fired for the namespace, project and component
// the SRE agent names, before acting on that claim.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TokenSource supplies aep-api's service bearer token.
type TokenSource interface {
	Token() (string, error)
}

// AlertQuerier queries the observer's recorded alerts.
type AlertQuerier struct {
	baseURL string
	tokens  TokenSource
	http    *http.Client
}

// NewAlertQuerier returns a querier for the observer at baseURL, authenticated
// with tokens.
func NewAlertQuerier(baseURL string, tokens TokenSource) *AlertQuerier {
	return &AlertQuerier{
		baseURL: strings.TrimRight(baseURL, "/"),
		tokens:  tokens,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

type alertsQueryRequest struct {
	StartTime   time.Time        `json:"startTime"`
	EndTime     time.Time        `json:"endTime"`
	Limit       int              `json:"limit"`
	SearchScope alertSearchScope `json:"searchScope"`
}

type alertSearchScope struct {
	Namespace string `json:"namespace"`
	Project   string `json:"project,omitempty"`
	Component string `json:"component,omitempty"`
}

// RecentAlert reports whether the observer recorded an alert for namespace,
// project and component (any component in the project when component is
// empty) since since. A scope the observer does not know, such as a namespace
// or component that does not exist, is false with no error; anything else the
// observer cannot answer is an error, so a caller can fail closed.
func (q *AlertQuerier) RecentAlert(ctx context.Context, namespace, project, component string, since time.Time) (bool, error) {
	token, err := q.tokens.Token()
	if err != nil {
		return false, fmt.Errorf("observer alerts: service token: %w", err)
	}
	body, err := json.Marshal(alertsQueryRequest{
		StartTime: since.UTC(),
		EndTime:   time.Now().UTC(),
		Limit:     1,
		SearchScope: alertSearchScope{
			Namespace: namespace, Project: project, Component: component,
		},
	})
	if err != nil {
		return false, fmt.Errorf("observer alerts: encode query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.baseURL+"/api/v1alpha1/alerts/query", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("observer alerts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := q.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("observer alerts: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, fmt.Errorf("observer alerts: read response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		var out struct {
			Alerts []json.RawMessage `json:"alerts"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return false, fmt.Errorf("observer alerts: decode response: %w", err)
		}
		return len(out.Alerts) > 0, nil
	case resp.StatusCode == http.StatusBadRequest && scopeNotFound(raw):
		return false, nil
	default:
		return false, fmt.Errorf("observer alerts: status %d", resp.StatusCode)
	}
}

// scopeNotFound reports whether a 400 is the observer's SCOPE_NOT_FOUND: one
// of the named resources does not exist.
func scopeNotFound(raw []byte) bool {
	var e struct {
		ErrorCode string `json:"errorCode"`
	}
	return json.Unmarshal(raw, &e) == nil && e.ErrorCode == "SCOPE_NOT_FOUND"
}
