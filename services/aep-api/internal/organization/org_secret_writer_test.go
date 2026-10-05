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
// at the writer's two ports (the fake repository keeps the real one's
// compare-and-swap semantics), so the write order, every rollback branch and
// the concurrent interleavings are observable call by call.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
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
	errStamp   = errors.New("stamp failed")
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

// CreateSecret rewrites the value under loc.RefName (the secrets client's
// in-place write, which mintingSM routes here).
func (v *fakeVault) CreateSecret(_ context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	if v.createErr != nil {
		return "", v.createErr
	}
	v.lastLoc, v.lastData = loc, maps.Clone(data)
	v.refs[loc.RefName] = true
	return loc.RefName, nil
}

func (v *fakeVault) live() []string {
	return slices.Sorted(maps.Keys(v.refs))
}

// fakeRepo keys rows "org/secret" and applies the real repository's
// compare-and-swap rules.
type fakeRepo struct {
	rows map[string]organization.OrgSecretRef

	upsertErr       error  // the next Upsert fails without writing
	upsertCommitErr error  // the next Upsert writes, then reports this error
	deleteErr       error  // every Delete fails
	beforeUpsert    func() // runs once, before the next Upsert
	onUpsert        func(organization.OrgSecretRef)
	onDelete        func()
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[string]organization.OrgSecretRef{}} }

func rowKey(org string, s organization.OrgSecret) string { return org + "/" + string(s) }

func (r *fakeRepo) set(org string, s organization.OrgSecret, name string) {
	r.rows[rowKey(org, s)] = organization.OrgSecretRef{Secret: s, Name: name}
}

func (r *fakeRepo) name(org string, s organization.OrgSecret) string {
	return r.rows[rowKey(org, s)].Name
}

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

func (r *fakeRepo) Upsert(_ context.Context, org string, ref organization.OrgSecretRef, expectPrev string) error {
	if hook := r.beforeUpsert; hook != nil {
		r.beforeUpsert = nil
		hook()
	}
	if r.onUpsert != nil {
		r.onUpsert(ref)
	}
	if err := r.upsertErr; err != nil {
		r.upsertErr = nil
		return err
	}
	cur, ok := r.rows[rowKey(org, ref.Secret)]
	if (expectPrev == "" && ok) || (expectPrev != "" && (!ok || cur.Name != expectPrev)) {
		return organization.ErrOrgSecretConflict
	}
	r.rows[rowKey(org, ref.Secret)] = ref
	if err := r.upsertCommitErr; err != nil {
		r.upsertCommitErr = nil
		return err
	}
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, org string, s organization.OrgSecret, name string) error {
	if r.onDelete != nil {
		r.onDelete()
	}
	if r.deleteErr != nil {
		return r.deleteErr
	}
	cur, ok := r.rows[rowKey(org, s)]
	if !ok || cur.Name != name {
		return organization.ErrOrgSecretConflict
	}
	delete(r.rows, rowKey(org, s))
	return nil
}

// fakeLock is a per-key mutex, so concurrent writers really serialize.
type fakeLock struct {
	mu   sync.Mutex
	keys map[string]*sync.Mutex
	err  error
	on   func(event string) // "lock" / "unlock", called while held
}

func newFakeLock() *fakeLock { return &fakeLock{keys: map[string]*sync.Mutex{}} }

func (l *fakeLock) Lock(_ context.Context, org string, s organization.OrgSecret) (func(), error) {
	if l.err != nil {
		return nil, l.err
	}
	l.mu.Lock()
	m, ok := l.keys[rowKey(org, s)]
	if !ok {
		m = &sync.Mutex{}
		l.keys[rowKey(org, s)] = m
	}
	l.mu.Unlock()
	m.Lock()
	if l.on != nil {
		l.on("lock")
	}
	return func() {
		if l.on != nil {
			l.on("unlock")
		}
		m.Unlock()
	}, nil
}

func newWriter(v *fakeVault, repo *fakeRepo) *organization.OrgSecretWriter {
	return organization.NewOrgSecretWriter(v, repo, newFakeLock(), fixedClock)
}

func writeAndRetire(v *fakeVault, repo *fakeRepo, s organization.OrgSecret, data map[string]string, legacyOld string, repoint func(string) error) (string, error) {
	return newWriter(v, repo).WriteAndRetire(ctx, "default", "ou-1", s, data, legacyOld, repoint)
}

var (
	pat = map[string]string{"token": "t"}
	key = map[string]string{"api-key": "k"}
)

func TestWriter_OrderAndOldDeletedByName(t *testing.T) {
	v, repo := newFakeVault("default-github-pat-00000001"), newFakeRepo()
	repo.set("default", organization.OrgSecretGitHubPAT, "default-github-pat-00000001")
	var order []string
	v.onCreate = func(string) { order = append(order, "create") }
	repo.onUpsert = func(organization.OrgSecretRef) { order = append(order, "upsert") }
	v.onDelete = func(n string) { order = append(order, "delete:"+n) }
	name, err := writeAndRetire(v, repo, organization.OrgSecretGitHubPAT, pat, "", func(n string) error {
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
	row := repo.rows["default/github-pat"]
	if row.Name != name || row.WrittenAt == nil || !row.WrittenAt.Equal(writtenAt) {
		t.Fatalf("row = %+v, want name %s stamped %v", row, name, writtenAt)
	}
	if got := v.live(); !slices.Equal(got, []string{name}) {
		t.Fatalf("live = %v, want only the new reference", got)
	}
}

func TestWriter_OldSurvivesUntilRetire(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	written, err := newWriter(v, repo).Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", noop)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.deleted) != 0 || repo.name("default", organization.OrgSecretDefaultKey) != written.Name {
		t.Fatalf("deleted=%v row=%v: Write records the new name and deletes nothing", v.deleted, repo.rows)
	}
	written.Retire(ctx)
	if !slices.Equal(v.deleted, []string{"old"}) {
		t.Fatalf("deleted %v, want the old reference after Retire", v.deleted)
	}
}

func TestWriter_LocationIsOrgNamespaceAndOU(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	if _, err := newWriter(v, repo).WriteAndRetire(ctx, "acme", "ou-1", organization.OrgSecretDefaultKey, key, "", noop); err != nil {
		t.Fatal(err)
	}
	want := secretmanagersvc.SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "acme", EntityName: "default-key"}
	if v.lastLoc != want {
		t.Fatalf("location = %+v, want %+v", v.lastLoc, want)
	}
}

func TestWriter_LegacyNameUsedOnFirstWrite(t *testing.T) {
	v, repo := newFakeVault("github-pat-secrets"), newFakeRepo()
	if _, err := writeAndRetire(v, repo, organization.OrgSecretGitHubPAT, pat, "github-pat-secrets", noop); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(v.deleted, "github-pat-secrets") {
		t.Fatal("the pre-phase-1 deterministic reference must be retired on the first write")
	}
}

func TestWriter_StoredNameWinsOverLegacy(t *testing.T) {
	v, repo := newFakeVault("stored"), newFakeRepo()
	repo.set("default", organization.OrgSecretGitHubPAT, "stored")
	if _, err := writeAndRetire(v, repo, organization.OrgSecretGitHubPAT, pat, "github-pat-secrets", noop); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v.deleted, []string{"stored"}) {
		t.Fatalf("deleted %v, want only the stored name", v.deleted)
	}
}

func TestWriter_FailedStampDeletesNewKeepsOld(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	_, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", func(string) error { return errStamp })
	if !errors.Is(err, errStamp) {
		t.Fatalf("err = %v, want the stamp failure", err)
	}
	if repo.name("default", organization.OrgSecretDefaultKey) != "old" || slices.Contains(v.deleted, "old") || !slices.Equal(v.live(), []string{"old"}) {
		t.Fatalf("row=%v deleted=%v live=%v", repo.rows, v.deleted, v.live())
	}
}

func TestWriter_FailedStampOnFirstWriteLeavesNoRow(t *testing.T) {
	v, repo := newFakeVault("github-pat-secrets"), newFakeRepo()
	if _, err := writeAndRetire(v, repo, organization.OrgSecretGitHubPAT, pat, "github-pat-secrets", func(string) error { return errStamp }); err == nil {
		t.Fatal("want error")
	}
	if len(repo.rows) != 0 || !slices.Equal(v.live(), []string{"github-pat-secrets"}) {
		t.Fatalf("rows=%v live=%v: no row, the legacy reference untouched", repo.rows, v.live())
	}
}

// An upsert that failed without telling whether it committed, and whose
// row restore finds nothing to move: the old row stays and the new
// reference is kept (an orphan in the log), never repointed to.
func TestWriter_FailedUpsertKeepsOldRow(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	repo.upsertErr = errors.New("db down")
	repointed := false
	_, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", func(string) error {
		repointed = true
		return nil
	})
	if err == nil || repointed {
		t.Fatalf("err=%v repointed=%v: want error, no repoint", err, repointed)
	}
	if repo.name("default", organization.OrgSecretDefaultKey) != "old" || slices.Contains(v.deleted, "old") || len(v.live()) != 2 {
		t.Fatalf("row=%v deleted=%v live=%v: old row and reference kept, new reference kept", repo.rows, v.deleted, v.live())
	}
}

// An upsert that committed but reported an error: the row names the new
// reference, so the rollback must put the old row back before deleting it.
func TestWriter_AmbiguousUpsertRestoresTheRow(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	repo.upsertCommitErr = errors.New("connection reset after commit")
	if _, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", noop); err == nil {
		t.Fatal("want error")
	}
	if repo.name("default", organization.OrgSecretDefaultKey) != "old" || !slices.Equal(v.live(), []string{"old"}) {
		t.Fatalf("row=%v live=%v: the row must name a live reference", repo.rows, v.live())
	}
}

func TestWriter_FailedRestoreKeepsNewReference(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	repo.deleteErr = errors.New("db down")
	if _, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", func(string) error { return errStamp }); err == nil {
		t.Fatal("want error")
	}
	row, ok := repo.rows["default/default-key"]
	if !ok || !slices.Equal(v.live(), []string{row.Name}) {
		t.Fatalf("row=%v live=%v: a row still naming the new reference must keep it", repo.rows, v.live())
	}
}

// Both writers read P; the other writer completes (P → N2, P retired)
// before this one records its row. This write must lose: its CAS fails, it
// deletes its own reference and leaves N2 alone.
func TestWriter_ConcurrentWriterFirstToTheRowWins(t *testing.T) {
	v, repo := newFakeVault("P"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "P")
	repo.beforeUpsert = func() {
		v.refs["N2"] = true
		repo.set("default", organization.OrgSecretDefaultKey, "N2")
		delete(v.refs, "P")
	}
	repointed := false
	_, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", func(string) error {
		repointed = true
		return nil
	})
	if !errors.Is(err, organization.ErrOrgSecretConflict) || repointed {
		t.Fatalf("err=%v repointed=%v: want ErrOrgSecretConflict before any repoint", err, repointed)
	}
	if repo.name("default", organization.OrgSecretDefaultKey) != "N2" || !slices.Equal(v.live(), []string{"N2"}) {
		t.Fatalf("row=%v live=%v: the winner's row and reference must survive", repo.rows, v.live())
	}
}

// A row moved off the new reference by something outside the lock: the
// rollback must neither put P back over it nor delete the new reference
// (whether anything reads it is unknown), so N1 is kept as a logged orphan.
func TestWriter_ConflictingRestoreKeepsNewReference(t *testing.T) {
	v, repo := newFakeVault("P"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "P")
	var n1 string
	_, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", func(n string) error {
		n1 = n
		v.refs["N3"] = true
		repo.set("default", organization.OrgSecretDefaultKey, "N3")
		return errStamp
	})
	if !errors.Is(err, errStamp) {
		t.Fatalf("err = %v", err)
	}
	if got := repo.name("default", organization.OrgSecretDefaultKey); got != "N3" {
		t.Fatalf("row names %q, want N3 left alone", got)
	}
	if want := []string{"N3", "P", n1}; !slices.Equal(v.live(), slices.Sorted(slices.Values(want))) || len(v.deleted) != 0 {
		t.Fatalf("deleted=%v live=%v: nothing may be deleted when the restore conflicts", v.deleted, v.live())
	}
}

// Two writes of one secret: the second waits for the first's whole sequence
// (row, failed repoint, rollback) before it reads the row, so the row always
// names a live reference and consumers are never repointed out of order.
func TestWriter_ConcurrentWritesSerialize(t *testing.T) {
	v, repo := newFakeVault("P"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "P")
	w := organization.NewOrgSecretWriter(v, repo, newFakeLock(), fixedClock)
	type result struct {
		name string
		err  error
	}
	second := make(chan result, 1)
	var repoints []string
	var n1 string
	_, err := w.WriteAndRetire(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", func(n string) error {
		n1 = n
		repoints = append(repoints, n)
		go func() {
			name, err := w.WriteAndRetire(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", func(n string) error {
				repoints = append(repoints, n)
				return nil
			})
			second <- result{name, err}
		}()
		select {
		case <-second:
			t.Fatal("the second write ran while the first held the lock")
		case <-time.After(50 * time.Millisecond):
		}
		return errStamp
	})
	if !errors.Is(err, errStamp) {
		t.Fatalf("first write: err = %v", err)
	}
	r := <-second
	if r.err != nil {
		t.Fatalf("second write: %v", r.err)
	}
	if !slices.Equal(repoints, []string{n1, r.name}) {
		t.Fatalf("repoints %v, want the first write's then the second's", repoints)
	}
	if repo.name("default", organization.OrgSecretDefaultKey) != r.name || !slices.Equal(v.live(), []string{r.name}) {
		t.Fatalf("row=%v live=%v: the row must name the one live reference", repo.rows, v.live())
	}
}

// A removal racing a write waits for it too, and then removes what the row
// names after the write's rollback.
func TestWriter_RemoveWaitsForAWrite(t *testing.T) {
	v, repo := newFakeVault("P"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "P")
	w := organization.NewOrgSecretWriter(v, repo, newFakeLock(), fixedClock)
	removed := make(chan error, 1)
	_, err := w.WriteAndRetire(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", func(string) error {
		go func() { removed <- w.Remove(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, nil) }()
		select {
		case <-removed:
			t.Fatal("the removal ran while the write held the lock")
		case <-time.After(50 * time.Millisecond):
		}
		return errStamp
	})
	if !errors.Is(err, errStamp) {
		t.Fatalf("write: err = %v", err)
	}
	if err := <-removed; err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(repo.rows) != 0 || len(v.live()) != 0 {
		t.Fatalf("rows=%v live=%v, want the secret gone and no reference left", repo.rows, v.live())
	}
}

func TestWriter_LockHeldFromReadThroughRetire(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	lock := newFakeLock()
	var order []string
	lock.on = func(e string) { order = append(order, e) }
	v.onCreate = func(string) { order = append(order, "create") }
	v.onDelete = func(n string) { order = append(order, "delete:"+n) }
	repo.onUpsert = func(organization.OrgSecretRef) { order = append(order, "upsert") }
	w := organization.NewOrgSecretWriter(v, repo, lock, fixedClock)
	if _, err := w.WriteAndRetire(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", func(string) error {
		order = append(order, "repoint")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock", "create", "upsert", "repoint", "delete:old", "unlock"}; !slices.Equal(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	order = nil
	first := repo.name("default", organization.OrgSecretDefaultKey)
	written, err := w.Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", noop)
	if err != nil {
		t.Fatal(err)
	}
	written.Retire(ctx)
	if want := []string{"lock", "create", "upsert", "unlock", "delete:" + first}; !slices.Equal(order, want) {
		t.Fatalf("order %v, want %v: Write releases the lock, the caller retires after its commit", order, want)
	}
}

func TestWriter_LockFailureWritesNothing(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	lock := newFakeLock()
	lock.err = errors.New("db down")
	w := organization.NewOrgSecretWriter(v, repo, lock, fixedClock)
	if _, err := w.WriteAndRetire(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", noop); err == nil || v.creates != 0 {
		t.Fatalf("err=%v creates=%d, want an error before any write", err, v.creates)
	}
	if err := w.Remove(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, nil); err == nil {
		t.Fatal("Remove: want the lock error")
	}
}

// The caller's Retire runs after the lock is released; if the row names the
// previous reference again by then, it must not be deleted.
func TestWriter_RetireSkipsWhenTheRowNamesTheOldReference(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	written, err := newWriter(v, repo).Write(ctx, "default", "ou-1", organization.OrgSecretDefaultKey, key, "", noop)
	if err != nil {
		t.Fatal(err)
	}
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	written.Retire(ctx)
	if len(v.deleted) != 0 {
		t.Fatalf("deleted %v: the row names old again", v.deleted)
	}
}

func TestWriter_SameNameNeverDeleted(t *testing.T) {
	v, repo := newFakeVault("cred-x"), newFakeRepo()
	v.fixedName = "cred-x" // a provider that returned the existing name
	repo.set("default", organization.OrgSecretGitHubPAT, "cred-x")
	if _, err := writeAndRetire(v, repo, organization.OrgSecretGitHubPAT, pat, "", noop); err != nil {
		t.Fatal(err)
	}
	if len(v.deleted) != 0 {
		t.Fatal("never delete the reference just written")
	}
	// Nor on rollback.
	if _, err := writeAndRetire(v, repo, organization.OrgSecretGitHubPAT, pat, "", func(string) error { return errStamp }); err == nil {
		t.Fatal("want error")
	}
	if len(v.deleted) != 0 {
		t.Fatalf("deleted %v: a rollback must not delete the reference the old row names", v.deleted)
	}
}

func TestWriter_FailedRetireDoesNotFailTheWrite(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	v.deleteErr = errors.New("vault down")
	name, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", noop)
	if err != nil {
		t.Fatalf("a failed retire must not fail the write: %v", err)
	}
	if repo.name("default", organization.OrgSecretDefaultKey) != name {
		t.Fatalf("row = %v, want %s", repo.rows, name)
	}
}

func TestWriter_CreateFailureTouchesNothing(t *testing.T) {
	v, repo := newFakeVault("old"), newFakeRepo()
	repo.set("default", organization.OrgSecretDefaultKey, "old")
	v.createErr = secretmanagersvc.ErrConflict
	_, err := writeAndRetire(v, repo, organization.OrgSecretDefaultKey, key, "", noop)
	if !errors.Is(err, secretmanagersvc.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if repo.name("default", organization.OrgSecretDefaultKey) != "old" || len(v.deleted) != 0 {
		t.Fatalf("row=%v deleted=%v", repo.rows, v.deleted)
	}
}

func TestWriter_GitHubPATWritesTokenAndPassword(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	data := map[string]string{"token": "t"}
	if _, err := writeAndRetire(v, repo, organization.OrgSecretGitHubPAT, data, "", noop); err != nil {
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
		if _, err := writeAndRetire(v, newFakeRepo(), c.s, c.data, "", noop); err == nil || v.creates != 0 {
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

func TestWriter_RemoveOrdersRowRepointReference(t *testing.T) {
	v, repo := newFakeVault("stored"), newFakeRepo()
	repo.set("default", organization.OrgSecretStudioClient, "stored")
	var order []string
	repo.onDelete = func() { order = append(order, "row") }
	v.onDelete = func(n string) { order = append(order, "delete:"+n) }
	w := newWriter(v, repo)
	if err := w.Remove(ctx, "default", "ou-1", organization.OrgSecretStudioClient, func() error {
		order = append(order, "repoint")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"row", "repoint", "delete:stored"}; !slices.Equal(order, want) || len(repo.rows) != 0 {
		t.Fatalf("order=%v rows=%v, want %v and no row", order, repo.rows, want)
	}
	if err := w.Remove(ctx, "default", "ou-1", organization.OrgSecretStudioClient, nil); err != nil || len(v.deleted) != 1 {
		t.Fatalf("removing an unset secret: err=%v deleted=%v, want a no-op", err, v.deleted)
	}
}

func TestWriter_RemoveFailedRepointPutsTheRowBack(t *testing.T) {
	v, repo := newFakeVault("stored"), newFakeRepo()
	repo.set("default", organization.OrgSecretStudioClient, "stored")
	err := newWriter(v, repo).Remove(ctx, "default", "ou-1", organization.OrgSecretStudioClient, func() error { return errStamp })
	if !errors.Is(err, errStamp) {
		t.Fatalf("err = %v", err)
	}
	if repo.name("default", organization.OrgSecretStudioClient) != "stored" || len(v.deleted) != 0 {
		t.Fatalf("rows=%v deleted=%v: the row and its reference must survive", repo.rows, v.deleted)
	}
}

func TestWriter_RemoveFailedReferenceDeleteStillUnsets(t *testing.T) {
	v, repo := newFakeVault("stored"), newFakeRepo()
	repo.set("default", organization.OrgSecretStudioClient, "stored")
	v.deleteErr = errors.New("vault down")
	if err := newWriter(v, repo).Remove(ctx, "default", "ou-1", organization.OrgSecretStudioClient, nil); err != nil {
		t.Fatalf("a failed reference delete must not fail the removal: %v", err)
	}
	if len(repo.rows) != 0 {
		t.Fatalf("rows=%v, want the secret unset", repo.rows)
	}
}

func TestWriter_WriteIfUnsetWritesOnlyTheFirstTime(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	w := newWriter(v, repo)
	secret := map[string]string{"secret": "s1"}
	wrote, err := w.WriteIfUnset(ctx, "default", "ou-1", organization.OrgSecretGitHubWebhookSecret, secret)
	if err != nil || !wrote {
		t.Fatalf("first write: wrote=%v err=%v", wrote, err)
	}
	first := repo.name("default", organization.OrgSecretGitHubWebhookSecret)
	wrote, err = w.WriteIfUnset(ctx, "default", "ou-1", organization.OrgSecretGitHubWebhookSecret, map[string]string{"secret": "s2"})
	if err != nil || wrote {
		t.Fatalf("second write: wrote=%v err=%v", wrote, err)
	}
	if v.creates != 1 || repo.name("default", organization.OrgSecretGitHubWebhookSecret) != first || v.deleted != nil {
		t.Fatalf("a set secret is kept: creates=%d row=%q deleted=%v", v.creates, repo.name("default", organization.OrgSecretGitHubWebhookSecret), v.deleted)
	}
}

func TestWriter_ConcurrentWriteIfUnsetWritesOnce(t *testing.T) {
	v, repo := newFakeVault(), newFakeRepo()
	w := newWriter(v, repo)
	var wg sync.WaitGroup
	results := make([]bool, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = w.WriteIfUnset(ctx, "default", "ou-1", organization.OrgSecretGitHubWebhookSecret, map[string]string{"secret": fmt.Sprint(i)})
		}()
	}
	wg.Wait()
	if results[0] == results[1] || v.creates != 1 {
		t.Fatalf("exactly one first write: results=%v creates=%d", results, v.creates)
	}
}
