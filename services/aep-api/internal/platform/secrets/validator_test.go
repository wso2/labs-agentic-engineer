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

package secrets

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Validator unit tests — exercise classification + cascade dispatch
// without touching Postgres, GitHub, or the resolver. The DB-backed
// election (electAndList) is covered by the dbtest tier.

type fakeProbes struct {
	rows        []ActiveRow
	patFn       func(ActiveRow) (string, string, string, error)
	recordFn    func(string, string, string, string) (bool, error)
	recordCalls int
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
func (f *fakeProbes) RecordIdentityFromGitHub(ctx context.Context, ocOrgID, login, name, email string) (bool, error) {
	f.recordCalls++
	if f.recordFn == nil {
		return false, nil
	}
	return f.recordFn(ocOrgID, login, name, email)
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

// A GitHub App installation row left from before App mode was removed is
// neither probed nor cascaded: the validator logs it and moves on.
func TestProcessRow_AppInstallationRowIsSkipped(t *testing.T) {
	probes := &fakeProbes{
		patFn: func(ActiveRow) (string, string, string, error) {
			t.Fatal("an app-installation row must not be probed")
			return "", "", "", nil
		},
	}
	v := &Validator{
		probes: probes,
		cascade: func(context.Context, string, string) error {
			t.Fatal("an app-installation row must not cascade")
			return nil
		},
	}
	summary := &RunSummary{}
	if err := v.processRow(context.Background(), ActiveRow{OcOrgID: "default", Kind: "app-installation"}, summary); err != nil {
		t.Fatalf("processRow: %v", err)
	}
	if *summary != (RunSummary{}) || probes.recordCalls != 0 {
		t.Fatalf("summary=%+v recordCalls=%d, want nothing touched", summary, probes.recordCalls)
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
