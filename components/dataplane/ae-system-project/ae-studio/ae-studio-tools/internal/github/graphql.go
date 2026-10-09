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

package github

// The GraphQL transport. GitHub's v4 API is a single POST endpoint, so this is
// one function; the operations that use it live with their REST siblings
// (milestones.go). It exists only where GraphQL answers a question REST cannot
// answer in one call — today, the milestone dispatch predicate, which needs
// label-filtered OPEN-issue counts that REST would charge a full issue listing
// for and that the milestone's own open_issues field gets wrong (it counts PRs).
//
// Auth goes through the same authHeaders path as every REST call.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// graphQL executes one GraphQL operation and decodes the response's `data` into
// out (nil out discards it).
//
// A GraphQL response is 200 with an envelope, so two failure modes exist and
// stay distinguishable: a non-200 is *HTTPStatusError (transport
// / auth), a populated errors[] is *GraphQLError carrying every
// entry — callers branch on the machine-readable type rather than parsing a
// message. Partial results (data alongside errors) are treated as failures:
// every operation here reads a value it cannot default.
func (c *Client) graphQL(ctx context.Context, query string, variables map[string]any, out any) error {
	payload := map[string]any{"query": query}
	if len(variables) > 0 {
		payload["variables"] = variables
	}
	r, err := c.send(ctx, http.MethodPost, c.graphqlEndpoint, payload)
	if err != nil {
		return err
	}
	if r.status != http.StatusOK {
		return statusError(r, c.graphqlEndpoint, time.Now())
	}

	var envelope struct {
		Errors []GraphQLErrorDetail `json:"errors"`
		Data   json.RawMessage      `json:"data"`
	}
	if err := json.Unmarshal(r.body, &envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return &GraphQLError{Errors: envelope.Errors, Query: query}
	}
	if out == nil {
		return nil
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("github graphql response carried neither data nor errors")
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}
