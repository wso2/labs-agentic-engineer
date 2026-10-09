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

package getaestudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

type fakeStatus struct {
	st  organization.AEStudioStatus
	err error
	org *string
}

func (f fakeStatus) Status(_ context.Context, org string) (organization.AEStudioStatus, error) {
	if f.org != nil {
		*f.org = org
	}
	return f.st, f.err
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGetAeStudio(t *testing.T) {
	cases := []struct {
		st   organization.AEStudioStatus
		want string
	}{
		{organization.AEStudioStatus{State: organization.AEStudioAbsent}, `{"state":"absent"}`},
		{organization.AEStudioStatus{State: organization.AEStudioProvisioning}, `{"state":"provisioning"}`},
		{organization.AEStudioStatus{State: organization.AEStudioReady, URLs: &organization.AEStudioURLs{DesignAgent: "http://d", Collab: "ws://c", Tools: "http://t"}},
			`{"state":"ready","urls":{"collab":"ws://c","designAgent":"http://d","tools":"http://t"}}`},
	}
	for _, c := range cases {
		var org string
		h := New(fakeStatus{st: c.st, org: &org})
		resp, err := h.GetAeStudio(tenant.WithBoundOrg(context.Background(), "default"), gen.GetAeStudioRequestObject{})
		if err != nil {
			t.Fatal(err)
		}
		if got := mustJSON(t, resp); got != c.want {
			t.Fatalf("got %s want %s", got, c.want)
		}
		if org != "default" {
			t.Fatalf("status read for org %q, want the bound org", org)
		}
	}
}

// A failed answer carries its reason; a reasonless one (nil) omits it.
func TestGetAeStudio_FailedCarriesReason(t *testing.T) {
	cases := []struct {
		reason organization.AEStudioFailReason
		want   string
	}{
		{organization.AEStudioFailTimeout, `{"reason":"timeout","state":"failed"}`},
		{organization.AEStudioFailError, `{"reason":"error","state":"failed"}`},
		{"", `{"state":"failed"}`},
	}
	for _, c := range cases {
		h := New(fakeStatus{st: organization.AEStudioStatus{State: organization.AEStudioFailed, Reason: c.reason}})
		resp, err := h.GetAeStudio(tenant.WithBoundOrg(context.Background(), "default"), gen.GetAeStudioRequestObject{})
		if err != nil || mustJSON(t, resp) != c.want {
			t.Fatalf("reason %q: got %v %v, want %s", c.reason, mustJSON(t, resp), err, c.want)
		}
	}
}

// captureLog routes the default logger into a buffer for the test's duration.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// A failed read answers a fixed-message 500 and leaves the cause in the log,
// keyed by event, org and error.
func TestGetAeStudio_ReadErrorIs500WithFixedMessageAndIsLogged(t *testing.T) {
	logs := captureLog(t)
	h := New(fakeStatus{err: errors.New("oc down")})
	_, err := h.GetAeStudio(tenant.WithBoundOrg(context.Background(), "default"), gen.GetAeStudioRequestObject{})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusInternalServerError || apiErr.Message != "failed to read AE Studio state" {
		t.Fatalf("got %v", err)
	}
	var rec map[string]any
	if jerr := json.Unmarshal(logs.Bytes(), &rec); jerr != nil {
		t.Fatalf("want one JSON log record, got %q: %v", logs, jerr)
	}
	if rec["msg"] != "ae_studio.status_read_failed" || rec["level"] != "ERROR" || rec["org"] != "default" || rec["error"] != "oc down" {
		t.Fatalf("log record = %v", rec)
	}
}

// With the tenant gate in LOG mode a claimless request reaches the handler
// with no bound org: it fails closed instead of reading an org named "".
func TestGetAeStudio_NoBoundOrgIs401(t *testing.T) {
	called := false
	h := New(statusFunc(func(context.Context, string) (organization.AEStudioStatus, error) {
		called = true
		return organization.AEStudioStatus{State: organization.AEStudioReady}, nil
	}))
	_, err := h.GetAeStudio(context.Background(), gen.GetAeStudioRequestObject{})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("got %v, want 401", err)
	}
	if called {
		t.Fatal("read AE Studio state with no bound org")
	}
}

type statusFunc func(context.Context, string) (organization.AEStudioStatus, error)

func (f statusFunc) Status(ctx context.Context, org string) (organization.AEStudioStatus, error) {
	return f(ctx, org)
}
