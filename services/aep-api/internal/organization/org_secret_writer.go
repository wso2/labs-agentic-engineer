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

package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
)

// OrgSecretVault is the part of secretmanagersvc.SecretManagementClient the
// writer uses: every write is a new SecretReference, every delete names one.
type OrgSecretVault interface {
	CreateSecretRef(ctx context.Context, location secretmanagersvc.SecretLocation, data map[string]string) (string, error)
	DeleteSecretRef(ctx context.Context, location secretmanagersvc.SecretLocation, name string) error
}

// OrgSecretWriter writes and removes the org secrets. A write never edits a
// live reference: it stores the value under a new reference, records it in
// the secret's row, lets the caller repoint the consumers, and only then
// deletes the previous reference by the name the row stored. Whatever fails
// before the old reference is deleted leaves the old reference and its row
// in place and deletes the new reference.
type OrgSecretWriter struct {
	vault OrgSecretVault
	repo  OrgSecretRepository
	now   func() time.Time
}

// NewOrgSecretWriter builds a writer over vault and repo; now stamps
// written_at.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func NewOrgSecretWriter(vault OrgSecretVault, repo OrgSecretRepository, now func() time.Time) *OrgSecretWriter {
	return &OrgSecretWriter{vault: vault, repo: repo, now: now}
}

// Write stores data as secret s of the org (ocOrgID = the org's OpenChoreo
// namespace, ouID = its Thunder OU) and returns the new reference name.
//
// Order: new reference → upsert the row → repoint(newName) → delete the
// previous reference (the row's, else legacyOld, a pre-phase-1 deterministic
// reference) by name. The previous reference is never deleted when it is the
// name just written. If the upsert or repoint fails, the previous row is
// restored, the new reference is deleted and the error returned. A failed
// delete of the previous reference does not fail the write: the orphan is
// logged by name. repoint may be nil when nothing consumes the secret yet.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) Write(ctx context.Context, ocOrgID, ouID string, s OrgSecret, data map[string]string, legacyOld string, repoint func(newName string) error) (string, error) {
	if err := validateOrgSecretData(s, data); err != nil {
		return "", err
	}
	loc, err := orgSecretLocation(ocOrgID, ouID, s)
	if err != nil {
		return "", err
	}
	prev, err := w.repo.Get(ctx, ocOrgID, s)
	if err != nil {
		return "", fmt.Errorf("org secret %s: read row: %w", s, err)
	}
	oldName := legacyOld
	if prev != nil {
		oldName = prev.Name
	}

	name, err := w.vault.CreateSecretRef(ctx, loc, s.RefData(data))
	if err != nil {
		return "", fmt.Errorf("org secret %s: write: %w", s, err)
	}

	if err := w.repo.Upsert(ctx, ocOrgID, OrgSecretRef{Secret: s, Name: name, WrittenAt: w.now()}); err != nil {
		return "", w.undo(ctx, loc, s, name, oldName, nil, fmt.Errorf("org secret %s: record row: %w", s, err))
	}
	if repoint != nil {
		if err := repoint(name); err != nil {
			restore := func() error { return w.restoreRow(ctx, ocOrgID, s, prev) }
			return "", w.undo(ctx, loc, s, name, oldName, restore, fmt.Errorf("org secret %s: repoint: %w", s, err))
		}
	}

	if oldName != "" && oldName != name {
		if err := w.vault.DeleteSecretRef(ctx, loc, oldName); err != nil {
			slog.WarnContext(ctx, "orgsecret.retire_failed", "secret", string(s), "name", oldName, "error", err)
		}
	}
	slog.InfoContext(ctx, "orgsecret.written", "secret", string(s), "name", name)
	return name, nil
}

// undo rolls a write back after its new reference exists: restore (when
// given) puts the previous row back, then the new reference is deleted
// unless it is the previous one. cause is returned, joined with any failure
// of the rollback itself.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) undo(ctx context.Context, loc secretmanagersvc.SecretLocation, s OrgSecret, name, oldName string, restore func() error, cause error) error {
	errs := []error{cause}
	if restore != nil {
		if err := restore(); err != nil {
			errs = append(errs, fmt.Errorf("restore row: %w", err))
			// The row may still name the new reference; deleting it would
			// leave the row pointing at nothing.
			slog.ErrorContext(ctx, "orgsecret.rollback_failed", "secret", string(s), "name", name)
			return errors.Join(errs...)
		}
	}
	if name != oldName {
		if err := w.vault.DeleteSecretRef(ctx, loc, name); err != nil {
			slog.WarnContext(ctx, "orgsecret.retire_failed", "secret", string(s), "name", name, "error", err)
			errs = append(errs, fmt.Errorf("delete new reference %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) restoreRow(ctx context.Context, ocOrgID string, s OrgSecret, prev *OrgSecretRef) error {
	if prev != nil {
		return w.repo.Upsert(ctx, ocOrgID, *prev)
	}
	return w.repo.Delete(ctx, ocOrgID, s)
}

// Remove unsets secret s: its reference is deleted by the stored name, then
// its row. A failed reference delete keeps the row, so a retry finds the
// name again. An unset secret is a no-op.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) Remove(ctx context.Context, ocOrgID, ouID string, s OrgSecret) error {
	loc, err := orgSecretLocation(ocOrgID, ouID, s)
	if err != nil {
		return err
	}
	prev, err := w.repo.Get(ctx, ocOrgID, s)
	if err != nil {
		return fmt.Errorf("org secret %s: read row: %w", s, err)
	}
	if prev == nil {
		return nil
	}
	if err := w.vault.DeleteSecretRef(ctx, loc, prev.Name); err != nil {
		return fmt.Errorf("org secret %s: delete reference %s: %w", s, prev.Name, err)
	}
	if err := w.repo.Delete(ctx, ocOrgID, s); err != nil {
		return fmt.Errorf("org secret %s: delete row: %w", s, err)
	}
	slog.InfoContext(ctx, "orgsecret.removed", "secret", string(s), "name", prev.Name)
	return nil
}

// orgSecretLocation addresses secret s of the org: the reference lives in
// the org's OpenChoreo namespace (its name prefix) and the value under the
// OU's vault path.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func orgSecretLocation(ocOrgID, ouID string, s OrgSecret) (secretmanagersvc.SecretLocation, error) {
	if s.Keys() == nil {
		return secretmanagersvc.SecretLocation{}, fmt.Errorf("unknown org secret %q", s)
	}
	if strings.TrimSpace(ocOrgID) == "" || strings.TrimSpace(ouID) == "" {
		return secretmanagersvc.SecretLocation{}, fmt.Errorf("org secret %s: org and OU are required", s)
	}
	return secretmanagersvc.SecretLocation{OrgName: ouID, ControlPlaneNamespace: ocOrgID, EntityName: string(s)}, nil
}

// validateOrgSecretData requires exactly s's keys, each with a value.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func validateOrgSecretData(s OrgSecret, data map[string]string) error {
	want := s.Keys()
	if want == nil {
		return fmt.Errorf("unknown org secret %q", s)
	}
	got := make([]string, 0, len(data))
	for k, v := range data {
		if v == "" {
			return fmt.Errorf("org secret %s: %s is empty", s, k)
		}
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		return fmt.Errorf("org secret %s needs keys %v, got %v", s, want, got)
	}
	return nil
}
