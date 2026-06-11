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

package credentials

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Validator unit tests — exercise classification + cascade dispatch
// without touching Postgres, GitHub, or the resolver. The DB-backed
// election (electAndList) is exercised in the integration tests via the
// /internal/credentials/_validator/tick endpoint.

type fakeProbes struct {
	rows         []ActiveRow
	patFn        func(ActiveRow) (string, string, string, error)
	appFn        func(ActiveRow) (string, error)
	recordFn     func(string, string, string, string) (bool, error)
	loginFn      func(string, string) error
	touchFn      func(string) error
	recordCalls  int
	loginUpdates int
	touchCalls   int
}

func (f *fakeProbes) ListActiveRows(ctx context.Context) ([]ActiveRow, error) {
	return f.rows, nil
}
func (f *fakeProbes) ProbePAT(ctx context.Context, row ActiveRow) (string, string, string, error) {
	if f.patFn == nil {
		return row.IdentityLogin, row.IdentityLogin, row.IdentityLogin + "@example.com", nil
	}
	return f.patFn(row)
}
func (f *fakeProbes) ProbeApp(ctx context.Context, row ActiveRow) (string, error) {
	if f.appFn == nil {
		return row.GitHubLogin, nil
	}
	return f.appFn(row)
}
func (f *fakeProbes) RecordIdentityFromGitHub(ctx context.Context, ocOrgID, login, name, email string) (bool, error) {
	f.recordCalls++
	if f.recordFn == nil {
		return false, nil
	}
	return f.recordFn(ocOrgID, login, name, email)
}
func (f *fakeProbes) UpdateGitHubLogin(ctx context.Context, ocOrgID, login string) error {
	f.loginUpdates++
	if f.loginFn == nil {
		return nil
	}
	return f.loginFn(ocOrgID, login)
}
func (f *fakeProbes) TouchValidatedAt(ctx context.Context, ocOrgID string) error {
	f.touchCalls++
	if f.touchFn == nil {
		return nil
	}
	return f.touchFn(ocOrgID)
}

// processRow is the unit under test. The DB-backed Run/RunOnce paths
// are exercised in integration tests; here we tap directly into the
// classification + cascade dispatch.

func TestProcessRow_PAT_Happy(t *testing.T) {
	probes := &fakeProbes{}
	v := &Validator{probes: probes}
	row := ActiveRow{OcOrgID: "default", Kind: "user-pat", IdentityLogin: "alice"}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), row, summary); err != nil {
		t.Fatalf("processRow: %v", err)
	}
	if summary.ValidatedRows != 1 || summary.DriftedRows != 0 || summary.CascadedRows != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if probes.recordCalls != 1 {
		t.Fatalf("expected one RecordIdentityFromGitHub call, got %d", probes.recordCalls)
	}
}

func TestProcessRow_PAT_Unauthorized_FiresCascade(t *testing.T) {
	probes := &fakeProbes{
		patFn: func(ActiveRow) (string, string, string, error) {
			return "", "", "", ErrCredentialUnauthorized
		},
	}
	cascadeCalls := 0
	v := &Validator{
		probes: probes,
		cascade: func(ctx context.Context, ocOrgID, cause string) error {
			cascadeCalls++
			if ocOrgID != "default" || cause != "validator.unauthorized" {
				t.Fatalf("unexpected cascade args: %s %s", ocOrgID, cause)
			}
			return nil
		},
	}
	row := ActiveRow{OcOrgID: "default", Kind: "user-pat"}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), row, summary); err != nil {
		t.Fatalf("processRow: %v", err)
	}
	if cascadeCalls != 1 {
		t.Fatalf("expected one cascade call, got %d", cascadeCalls)
	}
	if summary.CascadedRows != 1 {
		t.Fatalf("expected 1 cascaded row in summary, got %+v", summary)
	}
}

func TestProcessRow_PAT_Transient_NoCascade(t *testing.T) {
	probes := &fakeProbes{
		patFn: func(ActiveRow) (string, string, string, error) {
			return "", "", "", ErrCredentialTransient
		},
	}
	cascadeCalls := 0
	v := &Validator{
		probes: probes,
		cascade: func(ctx context.Context, _, _ string) error {
			cascadeCalls++
			return nil
		},
	}
	row := ActiveRow{OcOrgID: "default", Kind: "user-pat"}
	summary := &RunSummary{}
	err := v.processRow(context.Background(), row, summary)
	if !errors.Is(err, ErrCredentialTransient) {
		t.Fatalf("expected ErrCredentialTransient, got %v", err)
	}
	if cascadeCalls != 0 {
		t.Fatalf("transient error must not fire cascade")
	}
	if summary.CascadedRows != 0 || summary.ValidatedRows != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestProcessRow_PAT_IdentityDrift(t *testing.T) {
	probes := &fakeProbes{
		patFn: func(ActiveRow) (string, string, string, error) {
			return "alice-renamed", "Alice R", "alice@example.com", nil
		},
		recordFn: func(_, login, _, _ string) (bool, error) {
			return login == "alice-renamed", nil
		},
	}
	v := &Validator{probes: probes}
	row := ActiveRow{OcOrgID: "default", Kind: "user-pat", IdentityLogin: "alice"}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), row, summary); err != nil {
		t.Fatalf("processRow: %v", err)
	}
	if summary.DriftedRows != 1 {
		t.Fatalf("expected drift recorded, got %+v", summary)
	}
}

func TestProcessRow_App_Unauthorized_FiresCascade(t *testing.T) {
	probes := &fakeProbes{
		appFn: func(ActiveRow) (string, error) {
			return "", ErrCredentialUnauthorized
		},
	}
	cascadeCalls := 0
	v := &Validator{
		probes: probes,
		cascade: func(ctx context.Context, ocOrgID, cause string) error {
			cascadeCalls++
			return nil
		},
	}
	id := int64(42)
	row := ActiveRow{OcOrgID: "default", Kind: "app-installation", InstallationID: &id}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), row, summary); err != nil {
		t.Fatalf("processRow: %v", err)
	}
	if cascadeCalls != 1 || summary.CascadedRows != 1 {
		t.Fatalf("expected cascade fired once: calls=%d cascadedRows=%d", cascadeCalls, summary.CascadedRows)
	}
}

func TestProcessRow_App_Rename_DriftsLogin(t *testing.T) {
	probes := &fakeProbes{
		appFn: func(ActiveRow) (string, error) {
			return "asdlc-repos-renamed", nil
		},
	}
	v := &Validator{probes: probes}
	id := int64(42)
	row := ActiveRow{
		OcOrgID:        "default",
		Kind:           "app-installation",
		GitHubLogin:    "asdlc-repos",
		InstallationID: &id,
	}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), row, summary); err != nil {
		t.Fatalf("processRow: %v", err)
	}
	if probes.loginUpdates != 1 {
		t.Fatalf("expected one UpdateGitHubLogin call, got %d", probes.loginUpdates)
	}
	if probes.touchCalls != 1 {
		t.Fatalf("expected one TouchValidatedAt call, got %d", probes.touchCalls)
	}
	if summary.DriftedRows != 1 || summary.ValidatedRows != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestProcessRow_App_Stable_NoDrift(t *testing.T) {
	probes := &fakeProbes{}
	v := &Validator{probes: probes}
	id := int64(42)
	row := ActiveRow{
		OcOrgID:        "default",
		Kind:           "app-installation",
		GitHubLogin:    "asdlc-repos",
		InstallationID: &id,
	}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), row, summary); err != nil {
		t.Fatalf("processRow: %v", err)
	}
	if probes.loginUpdates != 0 {
		t.Fatalf("stable login must not trigger UpdateGitHubLogin")
	}
	if summary.DriftedRows != 0 || summary.ValidatedRows != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestProcessRow_NilCascadeLogsAndReturns(t *testing.T) {
	probes := &fakeProbes{
		patFn: func(ActiveRow) (string, string, string, error) {
			return "", "", "", ErrCredentialUnauthorized
		},
	}
	v := &Validator{probes: probes, cascade: nil}
	row := ActiveRow{OcOrgID: "default", Kind: "user-pat"}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), row, summary); err != nil {
		t.Fatalf("processRow with nil cascade should not error, got %v", err)
	}
	if summary.CascadedRows != 1 {
		t.Fatalf("CascadedRows should still be incremented even if cascade callback is nil")
	}
}

// New ensures NewValidator's defaults are sane.
func TestNewValidator_Defaults(t *testing.T) {
	v := NewValidator(nil, &fakeProbes{}, nil, 0)
	if v.interval != 24*time.Hour {
		t.Fatalf("default interval should be 24h, got %v", v.interval)
	}
}
