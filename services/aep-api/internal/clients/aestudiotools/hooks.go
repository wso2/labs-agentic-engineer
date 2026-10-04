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

package aestudiotools

// hooks.go — the repository webhook the platform registers (WebhookOps keyed
// by RepoRef). The pod signs it with the org's hook secret and points it at
// its own relay URL, so aep-api sends only the events.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
)

// hookEventNames are the only events the platform's hook carries (the pod's
// enum): anything else is refused before a request leaves.
var hookEventNames = map[string]gen.HookEventsRequestEvents{
	"pull_request":  gen.HookEventsRequestEventsPullRequest,
	"push":          gen.HookEventsRequestEventsPush,
	"issue_comment": gen.HookEventsRequestEventsIssueComment,
	"issues":        gen.HookEventsRequestEventsIssues,
}

func hookEvents(events []string) (gen.HookEventsRequest, error) {
	req := gen.HookEventsRequest{Events: make([]gen.HookEventsRequestEvents, 0, len(events))}
	for _, e := range events {
		v, ok := hookEventNames[e]
		if !ok {
			return gen.HookEventsRequest{}, fmt.Errorf("ae studio: hook event %q is not one the platform subscribes to", e)
		}
		req.Events = append(req.Events, v)
	}
	return req, nil
}

// RegisterWebhook ensures the platform's hook on ref carries events (created,
// or an existing one's events replaced) and answers its id.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) RegisterWebhook(ctx context.Context, ref RepoRef, events []string) (int64, error) {
	if err := validRef(ref); err != nil {
		return 0, err
	}
	body, err := hookEvents(events)
	if err != nil {
		return 0, err
	}
	var reply gen.Hook
	err = a.do(ctx, ref.Org, "register-hook", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.RegisterHook(ctx, ref.Owner, ref.Repo, &gen.RegisterHookParams{XImpersonateOrg: org}, body, auth)
	})
	return reply.ID, err
}

// UpdateWebhookEvents replaces the hook's events; an unknown hook is an
// HTTPStatusError 404.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) UpdateWebhookEvents(ctx context.Context, ref RepoRef, hookID int64, events []string) error {
	if err := validRef(ref); err != nil {
		return err
	}
	body, err := hookEvents(events)
	if err != nil {
		return err
	}
	return a.do(ctx, ref.Org, "update-hook-events", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.UpdateHookEvents(ctx, ref.Owner, ref.Repo, hookID, &gen.UpdateHookEventsParams{XImpersonateOrg: org}, body, auth)
	})
}

// DeleteWebhook removes the hook; one already gone is success.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) DeleteWebhook(ctx context.Context, ref RepoRef, hookID int64) error {
	if err := validRef(ref); err != nil {
		return err
	}
	return a.do(ctx, ref.Org, "delete-hook", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.DeleteHook(ctx, ref.Owner, ref.Repo, hookID, &gen.DeleteHookParams{XImpersonateOrg: org}, auth)
	})
}
