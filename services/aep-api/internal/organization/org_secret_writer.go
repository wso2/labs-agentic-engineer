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
// the secret's row, lets the caller repoint the consumers, and only after
// that is the previous reference deleted, by the name the row stored.
// Whatever fails before then leaves the previous reference in place and
// puts the previous row back; the new reference is deleted only when no row
// can still name it.
//
// Writes and removals of one (org, secret) hold its OrgSecretLock from the
// first read through the repoint, so they never interleave; the row writes
// are also compare-and-swap on the name read, as a backstop.
type OrgSecretWriter struct {
	vault OrgSecretVault
	repo  OrgSecretRepository
	lock  OrgSecretLock
	now   func() time.Time
}

// NewOrgSecretWriter builds a writer over vault, repo and lock; now stamps
// written_at.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func NewOrgSecretWriter(vault OrgSecretVault, repo OrgSecretRepository, lock OrgSecretLock, now func() time.Time) *OrgSecretWriter {
	return &OrgSecretWriter{vault: vault, repo: repo, lock: lock, now: now}
}

// OrgSecretWrite is a recorded write whose previous reference is still in
// place. The caller runs Retire once its consumers are committed to Name.
type OrgSecretWrite struct {
	// Name is the reference the secret's row now names.
	Name string

	vault  OrgSecretVault
	repo   OrgSecretRepository
	loc    secretmanagersvc.SecretLocation
	org    string
	secret OrgSecret
	old    string
}

// Retire deletes the previous reference by its stored name. It never
// deletes Name, and it deletes nothing while the secret's row names the
// previous reference again (a later write was rolled back to it). A failed
// delete is logged with the orphan's name and does not fail the write. Run
// it after the caller's own transaction commits.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w OrgSecretWrite) Retire(ctx context.Context) {
	if w.old == "" || w.old == w.Name {
		return
	}
	cur, err := w.repo.Get(ctx, w.org, w.secret)
	if err != nil {
		slog.WarnContext(ctx, "orgsecret.retire_failed", "secret", string(w.secret), "name", w.old, "error", err)
		return
	}
	if cur != nil && cur.Name == w.old {
		slog.InfoContext(ctx, "orgsecret.retire_skipped", "secret", string(w.secret), "name", w.old)
		return
	}
	if err := w.vault.DeleteSecretRef(ctx, w.loc, w.old); err != nil {
		slog.WarnContext(ctx, "orgsecret.retire_failed", "secret", string(w.secret), "name", w.old, "error", err)
		return
	}
	slog.InfoContext(ctx, "orgsecret.retired", "secret", string(w.secret), "name", w.old)
}

// Write stores data as secret s of the org (ocOrgID = the org's OpenChoreo
// namespace, ouID = its Thunder OU) and returns the recorded write; the
// previous reference stays until the caller runs Retire.
//
// Order: new reference → conditional upsert of the row (it must still name
// the reference read before the write) → repoint(newName). The previous
// reference is the row's, else legacyOld (a pre-phase-1 deterministic
// reference with no row). If the upsert or repoint fails (a concurrent
// write is ErrOrgSecretConflict), the previous row is put back if the row
// still names the new reference, the new reference is deleted and the error
// returned. repoint may be nil when nothing consumes the secret yet.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) Write(ctx context.Context, ocOrgID, ouID string, s OrgSecret, data map[string]string, legacyOld string, repoint func(newName string) error) (OrgSecretWrite, error) {
	loc, err := orgSecretWriteLocation(ocOrgID, ouID, s, data)
	if err != nil {
		return OrgSecretWrite{}, err
	}
	unlock, err := w.lock.Lock(ctx, ocOrgID, s)
	if err != nil {
		return OrgSecretWrite{}, err
	}
	defer unlock()
	return w.write(ctx, loc, ocOrgID, s, data, legacyOld, repoint)
}

// write is Write's sequence; the caller holds the secret's lock.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) write(ctx context.Context, loc secretmanagersvc.SecretLocation, ocOrgID string, s OrgSecret, data map[string]string, legacyOld string, repoint func(newName string) error) (OrgSecretWrite, error) {
	prev, err := w.repo.Get(ctx, ocOrgID, s)
	if err != nil {
		return OrgSecretWrite{}, fmt.Errorf("org secret %s: read row: %w", s, err)
	}
	prevName, oldName := "", legacyOld
	if prev != nil {
		prevName, oldName = prev.Name, prev.Name
	}

	name, err := w.vault.CreateSecretRef(ctx, loc, s.RefData(data))
	if err != nil {
		return OrgSecretWrite{}, fmt.Errorf("org secret %s: write: %w", s, err)
	}

	now := w.now()
	if err := w.repo.Upsert(ctx, ocOrgID, OrgSecretRef{Secret: s, Name: name, WrittenAt: &now}, prevName); err != nil {
		cause := fmt.Errorf("org secret %s: record row: %w", s, err)
		if errors.Is(err, ErrOrgSecretConflict) {
			// The row was never written: nothing can name the new reference.
			return OrgSecretWrite{}, w.deleteNew(ctx, loc, s, name, oldName, cause)
		}
		return OrgSecretWrite{}, w.undo(ctx, loc, ocOrgID, s, prev, name, oldName, cause)
	}
	if repoint != nil {
		if err := repoint(name); err != nil {
			return OrgSecretWrite{}, w.undo(ctx, loc, ocOrgID, s, prev, name, oldName, fmt.Errorf("org secret %s: repoint: %w", s, err))
		}
	}
	slog.InfoContext(ctx, "orgsecret.written", "secret", string(s), "name", name)
	return OrgSecretWrite{Name: name, vault: w.vault, repo: w.repo, loc: loc, org: ocOrgID, secret: s, old: oldName}, nil
}

// WriteAndRetire is Write followed by Retire under the same lock, for a
// caller with no transaction of its own to commit first. It returns the new
// name.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) WriteAndRetire(ctx context.Context, ocOrgID, ouID string, s OrgSecret, data map[string]string, legacyOld string, repoint func(newName string) error) (string, error) {
	loc, err := orgSecretWriteLocation(ocOrgID, ouID, s, data)
	if err != nil {
		return "", err
	}
	unlock, err := w.lock.Lock(ctx, ocOrgID, s)
	if err != nil {
		return "", err
	}
	defer unlock()
	written, err := w.write(ctx, loc, ocOrgID, s, data, legacyOld, repoint)
	if err != nil {
		return "", err
	}
	written.Retire(ctx)
	return written.Name, nil
}

// undo rolls a write back after its row may name the new reference. The
// previous row is put back only while the row names the new reference. The
// new reference is deleted only once the row has been moved off it; if the
// restore conflicts (the row names something else, so whether anything
// still reads the new reference is unknown) or fails, it is kept and named
// in the log. cause is returned, joined with any failure of the rollback.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) undo(ctx context.Context, loc secretmanagersvc.SecretLocation, ocOrgID string, s OrgSecret, prev *OrgSecretRef, name, oldName string, cause error) error {
	moved, err := w.restoreRow(ctx, ocOrgID, s, prev, name)
	if err != nil {
		slog.ErrorContext(ctx, "orgsecret.rollback_failed", "secret", string(s), "name", name)
		return errors.Join(cause, fmt.Errorf("restore row: %w", err))
	}
	if !moved {
		if name != oldName {
			slog.WarnContext(ctx, "orgsecret.orphan_kept", "secret", string(s), "name", name)
		}
		return cause
	}
	return w.deleteNew(ctx, loc, s, name, oldName, cause)
}

// deleteNew deletes a rolled-back write's new reference, unless it is the
// previous one. cause is returned, joined with a failed delete.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) deleteNew(ctx context.Context, loc secretmanagersvc.SecretLocation, s OrgSecret, name, oldName string, cause error) error {
	if name == oldName {
		return cause
	}
	if err := w.vault.DeleteSecretRef(ctx, loc, name); err != nil {
		slog.WarnContext(ctx, "orgsecret.rollback_delete_failed", "secret", string(s), "name", name, "error", err)
		return errors.Join(cause, fmt.Errorf("delete new reference %s: %w", name, err))
	}
	return cause
}

// restoreRow puts prev back (or removes the row when there was none) only
// while the row names name, and reports whether it moved the row off name.
// A row naming anything else is left alone (moved = false, no error).
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (w *OrgSecretWriter) restoreRow(ctx context.Context, ocOrgID string, s OrgSecret, prev *OrgSecretRef, name string) (bool, error) {
	var err error
	if prev != nil {
		err = w.repo.Upsert(ctx, ocOrgID, *prev, name)
	} else {
		err = w.repo.Delete(ctx, ocOrgID, s, name)
	}
	if errors.Is(err, ErrOrgSecretConflict) {
		return false, nil
	}
	return err == nil, err
}

// Remove unsets secret s under the secret's lock. Order: delete the row (only if it still names the
// reference read) → repoint() so consumers stop reading the secret → delete
// the reference by its stored name. A repoint failure puts the row back and
// returns the error; a failed reference delete is logged with the orphan's
// name and does not fail the removal. An unset secret is a no-op. repoint
// may be nil when nothing consumes the secret.
//
//deadcode:keep wired by Task 1.14 (the agents card removes the model keys)
func (w *OrgSecretWriter) Remove(ctx context.Context, ocOrgID, ouID string, s OrgSecret, repoint func() error) error {
	loc, err := orgSecretLocation(ocOrgID, ouID, s)
	if err != nil {
		return err
	}
	unlock, err := w.lock.Lock(ctx, ocOrgID, s)
	if err != nil {
		return err
	}
	defer unlock()
	prev, err := w.repo.Get(ctx, ocOrgID, s)
	if err != nil {
		return fmt.Errorf("org secret %s: read row: %w", s, err)
	}
	if prev == nil {
		return nil
	}
	if err := w.repo.Delete(ctx, ocOrgID, s, prev.Name); err != nil {
		return fmt.Errorf("org secret %s: delete row: %w", s, err)
	}
	if repoint != nil {
		if err := repoint(); err != nil {
			if rerr := w.repo.Upsert(ctx, ocOrgID, *prev, ""); rerr != nil && !errors.Is(rerr, ErrOrgSecretConflict) {
				slog.ErrorContext(ctx, "orgsecret.rollback_failed", "secret", string(s), "name", prev.Name)
				return errors.Join(fmt.Errorf("org secret %s: repoint: %w", s, err), fmt.Errorf("restore row: %w", rerr))
			}
			return fmt.Errorf("org secret %s: repoint: %w", s, err)
		}
	}
	if err := w.vault.DeleteSecretRef(ctx, loc, prev.Name); err != nil {
		slog.WarnContext(ctx, "orgsecret.retire_failed", "secret", string(s), "name", prev.Name, "error", err)
		return nil
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

// orgSecretWriteLocation validates a write of data as s and addresses it.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func orgSecretWriteLocation(ocOrgID, ouID string, s OrgSecret, data map[string]string) (secretmanagersvc.SecretLocation, error) {
	if err := validateOrgSecretData(s, data); err != nil {
		return secretmanagersvc.SecretLocation{}, err
	}
	return orgSecretLocation(ocOrgID, ouID, s)
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
