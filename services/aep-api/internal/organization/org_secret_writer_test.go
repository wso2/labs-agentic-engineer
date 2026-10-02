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

package organization_test

// UNIT tier for OrgSecretWriter: the vault and the row repository are faked
// at the writer's two ports, so the write order and every rollback branch are
// observable call by call.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
)

var (
	ctx        = context.Background()
	writtenAt  = time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	fixedClock = func() time.Time { return writtenAt }
	noop       = func(string) error { return nil }
)

// fakeVault mints new names (or fixedName) and tracks which references exist.
type fakeVault struct {
	fixedName string
	createErr error
	deleteErr error

	creates  int
	lastLoc  secretmanagersvc.SecretLocation
	lastData map[string]string
	refs     map[string]bool
	deleted  []string

	onCreate func(name string)
	onDelete func(name string)
}

func newFakeVault(existing ...string) *fakeVault {
	v := &fakeVault{refs: map[string]bool{}}
	for _, n := range existing {
		v.refs[n] = true
	}
	return v
}

func (v *fakeVault) CreateSecretRef(_ context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	if v.createErr != nil {
		return "", v.createErr
	}
	v.creates++
	v.lastLoc, v.lastData = loc, maps.Clone(data)
	name := v.fixedName
	if name == "" {
		name = fmt.Sprintf("%s-%s-%08x", loc.ControlPlaneNamespace, loc.EntityName, 0xa0000000+v.creates)
	}
	v.refs[name] = true
	if v.onCreate != nil {
		v.onCreate(name)
	}
	return name, nil
}

func (v *fakeVault) DeleteSecretRef(_ context.Context, loc secretmanagersvc.SecretLocation, name string) error {
	v.lastLoc = loc
	if v.onDelete != nil {
		v.onDelete(name)
	}
	if v.deleteErr != nil {
		return v.deleteErr
	}
	v.deleted = append(v.deleted, name)
	delete(v.refs, name)
	return nil
}

func (v *fakeVault) live() []string {
	return slices.Sorted(maps.Keys(v.refs))
}

// fakeRepo keys rows "org/secret".
type fakeRepo struct {
	rows      map[string]organization.OrgSecretRef
	upsertErr error // returned by the first Upsert only
	deleteErr error
	onUpsert  func(organization.OrgSecretRef)
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]organization.OrgSecretRef{}} }

func rowKey(org string, s organization.OrgSecret) string { return org + "/" + string(s) }

func (r *fakeRepo) Get(_ context.Context, org string, s organization.OrgSecret) (*organization.OrgSecretRef, error) {
	row, ok := r.rows[rowKey(org, s)]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (r *fakeRepo) List(_ context.Context, org string) ([]organization.OrgSecretRef, error) {
	var out []organization.OrgSecretRef
	for _, s := range organization.OrgSecrets() {
		if row, ok := r.rows[rowKey(org, s)]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *fakeRepo) Upsert(_ context.Context, org string, ref organization.OrgSecretRef) error {
	if r.onUpsert != nil {
		r.onUpsert(ref)
	}
	if err := r.upsertErr; err != nil {
		r.upsertErr = nil
		return err
	}
	r.rows[rowKey(org, ref.Secret)] = ref
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, org string, s organization.OrgSecret) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.rows, rowKey(org, s))
	return nil
}

func TestWriter_OrderAndOldDeletedByName(t *testing.T) {
	v, repo := newFakeVault("default-github-pat-00000001"), newFakeRepo()
	repo.rows["default/github-pat"] = organization.OrgSecretRef{Secret: organization.OrgSecretGitHubPAT, Name: "default-github-pat-00000001"}
	w := organization.NewOrgSecretWriter(v, repo, fixedClock)
	var order []string
	v.onCreate = func(string) { order = append(order, "create") }
	repo.onUpsert = func(organization.OrgSecretRef) { order = append(order, "upsert") }
	v.onDelete = func(n string) { order = append(order, "delete:"+n) }
	name, err := w.Write(ctx, "default", "ou-1", organization.OrgSecretGitHubPAT, map[string]string{"token": "t"}, "", func(n string) error {
		order = append(order, "repoint:"+n)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create", "upsert", "repoint:" + name, "delete:default-github-pat-00000001"}
	if !slices.Equal(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	if got := repo.rows["default/github-pat"]; got.Name != name || !got.WrittenAt.Equal(writtenAt) {
		t.Fatalf("row = %+v, want name %s stamped %v", got, name, writtenAt)
	}
	if got := v.live(); !slices.Equal(got, []string{name}) {
		t.Fatalf("live = %v, want only the new reference", got)
	}
}

func TestWriter_LocationIsOrgNamespaceAndOU(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	if _, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "acme", "ou-1", organization.OrgSecretDefaultKey, map[string]string{"api-key": "k"}, "", noop); err != nil {
		t.Fatal(err)
	}
	want := secretmanagersvc.SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "acme", EntityName: "default-key"}
	if v.lastLoc != want {
		t.Fatalf("location = %+v, want %+v", v.lastLoc, want)
	}
}

func TestWriter_LegacyNameUsedOnFirstWrite(t *testing.T) {
	v, repo := newFakeVault("github-pat-secrets"), newFakeRepo()
	w := organization.NewOrgSecretWriter(v, repo, fixedClock)
	if _, err := w.Write(ctx, "default", "ou-1", organization.OrgSecretGitHubPAT, map[string]string{"token": "t"}, "github-pat-secrets", noop); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(v.deleted, "github-pat-secrets") {
		t.Fatal("the pre-phase-1 deterministic reference must be retired on the first write")
	}
}

func TestWriter_StoredNameWinsOverLegacy(t *testing.T) {
	v, repo := newFakeVault("stored"), newFakeRepo()
	repo.rows["default/github-pat"] = organization.OrgSecretRef{Secret: organization.OrgSecretGitHubPAT, Name: "stored"}
	if _, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretGitHubPAT, map[string]string{"token": "t"}, "github-pat-secrets", noop); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v.deleted, []string{"stored"}) {
		t.Fatalf("deleted %v, want only the stored name", v.deleted)
	}
}

func TestWriter_FailedStampDeletesNewKeepsOld(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.rows["default/default-key"] = organization.OrgSecretRef{Secret: organization.OrgSecretDefaultKey, Name: "old"}
	w := organization.NewOrgSecretWriter(v, repo, fixedClock)
	_, err := w.Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, map[string]string{"api-key": "k"}, "", func(string) error { return errors.New("stamp failed") })
	if err == nil {
		t.Fatal("want error")
	}
	if repo.rows["default/default-key"].Name != "old" || slices.Contains(v.deleted, "old") || !slices.Equal(v.live(), []string{"old"}) {
		t.Fatalf("row=%v deleted=%v live=%v", repo.rows, v.deleted, v.live())
	}
}

func TestWriter_FailedStampOnFirstWriteLeavesNoRow(t *testing.T) {
	v, repo := newFakeVault("github-pat-secrets"), newFakeRepo()
	_, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretGitHubPAT, map[string]string{"token": "t"}, "github-pat-secrets", func(string) error { return errors.New("stamp failed") })
	if err == nil {
		t.Fatal("want error")
	}
	if len(repo.rows) != 0 || !slices.Equal(v.live(), []string{"github-pat-secrets"}) {
		t.Fatalf("rows=%v live=%v: no row, the legacy reference untouched", repo.rows, v.live())
	}
}

func TestWriter_FailedUpsertDeletesNewKeepsOld(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.rows["default/default-key"] = organization.OrgSecretRef{Secret: organization.OrgSecretDefaultKey, Name: "old"}
	repo.upsertErr = errors.New("db down")
	repointed := false
	_, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, map[string]string{"api-key": "k"}, "", func(string) error {
		repointed = true
		return nil
	})
	if err == nil || repointed {
		t.Fatalf("err=%v repointed=%v: want error, no repoint", err, repointed)
	}
	if repo.rows["default/default-key"].Name != "old" || !slices.Equal(v.live(), []string{"old"}) {
		t.Fatalf("row=%v live=%v", repo.rows, v.live())
	}
}

func TestWriter_FailedRestoreKeepsNewReference(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	repo.deleteErr = errors.New("db down")
	_, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, map[string]string{"api-key": "k"}, "", func(string) error { return errors.New("stamp failed") })
	if err == nil {
		t.Fatal("want error")
	}
	row, ok := repo.rows["default/default-key"]
	if !ok || !slices.Equal(v.live(), []string{row.Name}) {
		t.Fatalf("row=%v live=%v: a row still naming the new reference must keep it", repo.rows, v.live())
	}
}

func TestWriter_SameNameNeverDeleted(t *testing.T) {
	v, repo := newFakeVault("cred-x"), newFakeRepo()
	v.fixedName = "cred-x" // a provider that returned the existing name
	repo.rows["default/github-pat"] = organization.OrgSecretRef{Secret: organization.OrgSecretGitHubPAT, Name: "cred-x"}
	if _, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretGitHubPAT, map[string]string{"token": "t"}, "", noop); err != nil {
		t.Fatal(err)
	}
	if len(v.deleted) != 0 {
		t.Fatal("never delete the reference just written")
	}
	// Nor on rollback.
	if _, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretGitHubPAT, map[string]string{"token": "t"}, "", func(string) error { return errors.New("stamp failed") }); err == nil {
		t.Fatal("want error")
	}
	if len(v.deleted) != 0 {
		t.Fatalf("deleted %v: a rollback must not delete the reference the old row names", v.deleted)
	}
}

func TestWriter_FailedRetireDoesNotFailTheWrite(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.rows["default/default-key"] = organization.OrgSecretRef{Secret: organization.OrgSecretDefaultKey, Name: "old"}
	v.deleteErr = errors.New("vault down")
	name, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, map[string]string{"api-key": "k"}, "", noop)
	if err != nil {
		t.Fatalf("a failed retire must not fail the write: %v", err)
	}
	if repo.rows["default/default-key"].Name != name {
		t.Fatalf("row = %v, want %s", repo.rows, name)
	}
}

func TestWriter_CreateFailureTouchesNothing(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.rows["default/default-key"] = organization.OrgSecretRef{Secret: organization.OrgSecretDefaultKey, Name: "old"}
	v.createErr = secretmanagersvc.ErrConflict
	_, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, map[string]string{"api-key": "k"}, "", noop)
	if !errors.Is(err, secretmanagersvc.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if repo.rows["default/default-key"].Name != "old" || len(v.deleted) != 0 {
		t.Fatalf("row=%v deleted=%v", repo.rows, v.deleted)
	}
}

func TestWriter_GitHubPATWritesTokenAndPassword(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	data := map[string]string{"token": "t"}
	if _, err := organization.NewOrgSecretWriter(v, repo, fixedClock).Write(ctx, "default", "ou-1", organization.OrgSecretGitHubPAT, data, "", noop); err != nil {
		t.Fatal(err)
	}
	if got := v.lastData; len(got) != 2 || got["token"] != "t" || got["password"] != "t" || v.creates != 1 {
		t.Fatalf("data = %v creates = %d: one write carries token and password (the OC build checkout reads password, C9)", got, v.creates)
	}
	if len(data) != 1 {
		t.Fatalf("caller's data modified: %v", data)
	}
}

func TestWriter_RejectsWrongKeys(t *testing.T) {
	cases := []struct {
		s    organization.OrgSecret
		data map[string]string
	}{
		{organization.OrgSecretPublisherClient, map[string]string{"client_id": "a"}},
		{organization.OrgSecretGitHubPAT, map[string]string{"token": "t", "password": "t"}},
		{organization.OrgSecretDefaultKey, map[string]string{"api-key": ""}},
		{organization.OrgSecret("github/pat"), map[string]string{"token": "t"}},
	}
	for _, c := range cases {
		v := newFakeVault()
		if _, err := organization.NewOrgSecretWriter(v, newFakeRepo(), fixedClock).Write(ctx, "default", "ou-1", c.s, c.data, "", noop); err == nil || v.creates != 0 {
			t.Errorf("%s %v: err=%v creates=%d, want rejected before any write", c.s, slices.Sorted(maps.Keys(c.data)), err, v.creates)
		}
	}
}

func TestOrgSecret_Keys(t *testing.T) {
	want := map[organization.OrgSecret][]string{
		"github-pat":            {"token"},
		"github-webhook-secret": {"secret"},
		"default-key":           {"api-key"},
		"coding-agent-key":      {"api-key"},
		"ae-publisher-client":   {"client_id", "client_secret"},
		"ae-studio-client":      {"client_id", "client_secret"},
	}
	if got := organization.OrgSecrets(); len(got) != len(want) {
		t.Fatalf("OrgSecrets() = %v", got)
	}
	for _, s := range organization.OrgSecrets() {
		if !slices.Equal(s.Keys(), want[s]) {
			t.Errorf("%s.Keys() = %v, want %v", s, s.Keys(), want[s])
		}
	}
}

func TestWriter_RemoveDeletesStoredNameThenRow(t *testing.T) {
	v, repo := newFakeVault("stored"), newFakeRepo()
	repo.rows["default/ae-studio-client"] = organization.OrgSecretRef{Secret: organization.OrgSecretStudioClient, Name: "stored"}
	w := organization.NewOrgSecretWriter(v, repo, fixedClock)
	if err := w.Remove(ctx, "default", "ou-1", organization.OrgSecretStudioClient); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v.deleted, []string{"stored"}) || len(repo.rows) != 0 {
		t.Fatalf("deleted=%v rows=%v", v.deleted, repo.rows)
	}
	if err := w.Remove(ctx, "default", "ou-1", organization.OrgSecretStudioClient); err != nil || len(v.deleted) != 1 {
		t.Fatalf("removing an unset secret: err=%v deleted=%v, want a no-op", err, v.deleted)
	}
}

func TestWriter_RemoveKeepsRowWhenDeleteFails(t *testing.T) {
	v, repo := newFakeVault("stored"), newFakeRepo()
	repo.rows["default/ae-studio-client"] = organization.OrgSecretRef{Secret: organization.OrgSecretStudioClient, Name: "stored"}
	v.deleteErr = errors.New("vault down")
	if err := organization.NewOrgSecretWriter(v, repo, fixedClock).Remove(ctx, "default", "ou-1", organization.OrgSecretStudioClient); err == nil {
		t.Fatal("want error")
	}
	if repo.rows["default/ae-studio-client"].Name != "stored" {
		t.Fatalf("rows=%v: the row must survive so a retry finds the name", repo.rows)
	}
}
