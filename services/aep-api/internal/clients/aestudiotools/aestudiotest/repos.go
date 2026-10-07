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

package aestudiotest

// repos.go — a repository's lifecycle and attachments: create (RepoAdmin),
// trash, webhooks (WebhookOps), skills-mirror calls and reference documents.

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"path"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// HookEvents answers ref's webhooks: hook id → subscribed events.
func (f *Fake) HookEvents(ref sourcecontrol.RepoRef) map[int64][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[int64][]string{}
	for id, events := range f.state(ref).hooks {
		out[id] = slices.Clone(events)
	}
	return out
}

// References lists the file names stored for ref by the last accepted
// upload, in upload order.
func (f *Fake) References(ref sourcecontrol.RepoRef) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.references[keyOf(ref)])
}

// CreateOrgRepo creates ref.Repo under ref.Owner with an empty initial
// commit and answers its clone URL. A taken name is ErrRepoNameConflict,
// unless req.AdoptExisting, which answers the existing repository.
func (f *Fake) CreateOrgRepo(_ context.Context, ref sourcecontrol.RepoRef, req sourcecontrol.CreateOrgRepoRequest) (string, error) {
	if err := f.begin(Call{Op: OpCreateRepo, Ref: ref, Repo: req}); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(ref)
	cloneURL := "https://github.com/" + ref.Owner + "/" + ref.Repo + ".git"
	if st.created {
		if req.AdoptExisting {
			return cloneURL, nil
		}
		return "", sourcecontrol.ErrRepoNameConflict
	}
	st.created = true
	st.head = st.addCommit("", nil)
	return cloneURL, nil
}

// TrashRepo drops ref's repository, its issue side and its reference
// documents; later reads answer ErrRepoNotFound. A missing repository is
// success.
func (f *Fake) TrashRepo(_ context.Context, ref sourcecontrol.RepoRef) error {
	if err := f.begin(Call{Op: OpTrashRepo, Ref: ref}); err != nil {
		return err
	}
	f.mu.Lock()
	hook := f.beforeTrash
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.repos, keyOf(ref))
	delete(f.references, keyOf(ref))
	return nil
}

// RegisterWebhook ensures the hook delivering to the pod's URL and answers
// its id: an existing one is answered with its events replaced by these, as
// the pod's register-hook ("one call is an ensure") does.
func (f *Fake) RegisterWebhook(_ context.Context, ref sourcecontrol.RepoRef, events []string) (int64, error) {
	st, unlock, err := f.issueOp(OpRegisterWebhook, ref)
	if err != nil {
		return 0, err
	}
	defer unlock()
	if _, ok := st.hooks[st.podHook]; ok {
		st.hooks[st.podHook] = slices.Clone(events)
		return st.podHook, nil
	}
	f.nextHookID++
	st.hooks[f.nextHookID] = slices.Clone(events)
	st.podHook = f.nextHookID
	return f.nextHookID, nil
}

// SeedHook installs another integration's hook (not to the pod's URL) on
// ref and answers its id.
func (f *Fake) SeedHook(ref sourcecontrol.RepoRef, events []string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextHookID++
	f.state(ref).hooks[f.nextHookID] = slices.Clone(events)
	return f.nextHookID
}

// DeleteWebhook removes the hook; one already gone is success.
func (f *Fake) DeleteWebhook(_ context.Context, ref sourcecontrol.RepoRef, hookID int64) error {
	st, unlock, err := f.issueOp(OpDeleteWebhook, ref)
	if err != nil {
		return err
	}
	defer unlock()
	delete(st.hooks, hookID)
	return nil
}

// MirrorSkills records the call (Calls: Ref = project, Skills, Pinned) and
// answers the project's tip unchanged. It copies nothing: which library files
// a mirror writes is the pod's catalog rule, tested in ae-studio-tools, so
// this Fake keeps no second copy of it. Inject outcomes with FailOp.
func (f *Fake) MirrorSkills(_ context.Context, project, skills sourcecontrol.RepoRef, pinned []string) (sourcecontrol.CommitResult, error) {
	if err := f.begin(Call{Op: OpMirrorSkills, Ref: project, Skills: skills, Pinned: slices.Clone(pinned)}); err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.content(project)
	if err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	return sourcecontrol.CommitResult{CommitSHA: st.head}, nil
}

// ListReferences lists the names stored for ref as the pod stores them: each
// upload name reduced to a bare, lower-case name, sorted; empty when none.
func (f *Fake) ListReferences(_ context.Context, ref sourcecontrol.RepoRef) ([]string, error) {
	if err := f.begin(Call{Op: OpListReferences, Ref: ref}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	names := []string{}
	for _, n := range f.references[keyOf(ref)] {
		names = append(names, storedReferenceName(n))
	}
	slices.Sort(names)
	return names, nil
}

// storedReferenceName is the name the pod stores an upload under
// (ae-studio-tools edge.sanitizeReferenceName): the base name, its stem
// lower-cased with every character outside [a-z0-9._-] a dash, the
// extension lower-cased.
func storedReferenceName(raw string) string {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/"))
	ext := strings.ToLower(path.Ext(name))
	stem := strings.TrimSuffix(name, path.Ext(name))
	var b strings.Builder
	for _, r := range strings.ToLower(stem) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-.")
	if cleaned == "" {
		cleaned = "document"
	}
	return cleaned + ext
}

// PutReferences reads the multipart upload of field `files` and stores its
// file names for ref, replacing the previous set. It closes body, as the
// adapter does.
func (f *Fake) PutReferences(_ context.Context, ref sourcecontrol.RepoRef, contentType string, body io.Reader) error {
	if c, ok := body.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}
	if err := f.begin(Call{Op: OpPutReferences, Ref: ref}); err != nil {
		return err
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return errors.New("aestudiotest: not a multipart content type")
	}
	mr := multipart.NewReader(body, params["boundary"])
	var names []string
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if p.FormName() != "files" {
			return errors.New("aestudiotest: unexpected field " + p.FormName())
		}
		if _, err := io.Copy(io.Discard, p); err != nil {
			return err
		}
		names = append(names, p.FileName())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.references[keyOf(ref)] = names
	return nil
}
