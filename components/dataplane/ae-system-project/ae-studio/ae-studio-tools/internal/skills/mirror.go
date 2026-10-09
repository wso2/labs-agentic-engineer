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

package skills

// The project skill mirror (moved from aep-api's spec/skill_mirror.go): the
// pod is the single writer of `.claude/skills/` in a project repository, a
// copy of the coding-relevant slice of the org's skill library, so a build
// (and a developer who clones the repo) has the guidance locally.
//
// copied = (audience includes coding AND enabled) OR pinned by a component.
// The pin union is a drift guard: a skill disabled after a component pinned
// it still lands, so an admin toggle never breaks a build. aep-api reads the
// pins from the project's design.json files and passes them in.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// claudeSkillsDir is the mirrored tree's root in a project repository.
const claudeSkillsDir = ".claude/skills"

// syncMessage is the mirror commit's message (aep-api's, unchanged).
const syncMessage = "chore(skills): refresh .claude/skills from the org library"

// mirrorAttempts bounds the recompute after a commit conflict (the project
// tree changed under .claude/skills between the read and the commit).
const mirrorAttempts = 3

// errCatalogUnreadable marks a failure reading the skills repository (as
// opposed to the project), so the answer and its log name that repository.
var errCatalogUnreadable = errors.New("skills catalog unreadable")

// Workspace is the part of the engine the mirror uses.
type Workspace interface {
	BundleReader
	List(ctx context.Context, ref repo.RepoRef, at string) (entries []repo.Entry, headSHA string, err error)
	Commit(ctx context.Context, ref repo.RepoRef, writes []repo.CommitWrite, deletes []repo.CommitDelete, message string, author, committer *repo.GitIdentity) (repo.CommitResult, []repo.Conflict, error)
}

// Mirror writes the skill mirror into project repositories.
type Mirror struct {
	ws Workspace
	// identity authors the commit; nil (or a failed lookup) commits as the
	// engine's AEP default.
	identity repo.IdentitySource
}

// Option configures a Mirror.
type Option func(*Mirror)

// WithAuthor authors mirror commits as the gitpat user.
func WithAuthor(src repo.IdentitySource) Option {
	return func(m *Mirror) { m.identity = src }
}

// NewMirror mirrors through ws.
func NewMirror(ws Workspace, opts ...Option) Mirror {
	m := Mirror{ws: ws}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// Mirror makes project's `.claude/skills/` the desired mirror of the skills
// repository's tip for pinned: one commit writing every desired path that
// differs and deleting every other path under `.claude/skills/`, each pinned
// to the blob it read. Nothing differing commits nothing (Changed false, the
// project's tip). The library is read first and a failed read returns before
// the project is touched: pruning on a degraded read would delete a build's
// guidance. A conflict (the tree moved under `.claude/skills/` since the
// read) recomputes against the new tip, up to mirrorAttempts times, then
// returns repo.ErrCommitConflict.
func (m Mirror) Mirror(ctx context.Context, project, skillsRepo repo.RepoRef, pinned []string) (repo.CommitResult, error) {
	lib, err := LoadCatalog(ctx, m.ws, skillsRepo)
	if err != nil {
		return repo.CommitResult{}, fmt.Errorf("%w: %w", errCatalogUnreadable, err)
	}
	pins := make(map[string]bool, len(pinned))
	for _, p := range pinned {
		pins[p] = true
	}
	desired := desiredMirror(lib, pins)
	author := repo.AuthorOf(ctx, m.identity, "skills-mirror")

	for attempt := 1; ; attempt++ {
		entries, head, err := m.ws.List(ctx, project, "")
		if err != nil {
			return repo.CommitResult{}, fmt.Errorf("list project tree: %w", err)
		}
		writes, deletes := mirrorChanges(desired, entries)
		if len(writes)+len(deletes) == 0 {
			return repo.CommitResult{CommitSHA: head, Changed: false}, nil
		}
		res, _, err := m.ws.Commit(ctx, project, writes, deletes, syncMessage, author, nil)
		if errors.Is(err, repo.ErrCommitConflict) && attempt < mirrorAttempts {
			slog.WarnContext(ctx, "skills.mirror_conflict", "repo", strings.ToLower(project.FullName()), "attempt", attempt)
			continue
		}
		return res, err
	}
}

// mirrorChanges is what turns the tree (entries) into desired: a write for
// every desired path whose content differs (baseSha the current blob, ""
// when absent) and a delete for every path under `.claude/skills/` not
// desired, both in path order.
func mirrorChanges(desired map[string][]byte, entries []repo.Entry) ([]repo.CommitWrite, []repo.CommitDelete) {
	current := make(map[string]string, len(entries))
	var deletes []repo.CommitDelete
	for _, e := range entries {
		current[e.Path] = e.SHA
		if e.Path != claudeSkillsDir && !strings.HasPrefix(e.Path, claudeSkillsDir+"/") {
			continue
		}
		if _, ok := desired[e.Path]; !ok {
			deletes = append(deletes, repo.CommitDelete{Path: e.Path, BaseSHA: e.SHA})
		}
	}
	paths := make([]string, 0, len(desired))
	for p := range desired {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var writes []repo.CommitWrite
	for _, p := range paths {
		if cur, ok := current[p]; !ok || cur != repo.BlobSHA(desired[p]) {
			writes = append(writes, repo.CommitWrite{Path: p, Content: desired[p], BaseSHA: cur})
		}
	}
	sort.Slice(deletes, func(i, j int) bool { return deletes[i].Path < deletes[j].Path })
	return writes, deletes
}

// desiredMirror is the `.claude/skills/` tree for one project: every enabled
// coding-audience skill, plus every pinned one (a pin overrides both the
// audience and the enabled flag). Paths are `.claude/skills/<name>/SKILL.md`
// and each reference at `.claude/skills/<name>/<refPath>`.
func desiredMirror(lib []Skill, pinned map[string]bool) map[string][]byte {
	out := map[string][]byte{}
	for _, sk := range lib {
		if !(sk.Enabled && audienceIncludesCoding(sk.Audience)) && !pinned[sk.Name] {
			continue
		}
		dir := claudeSkillsDir + "/" + sk.Name
		out[dir+"/"+skillFileName] = []byte(sk.SkillMD)
		for refPath, content := range sk.References {
			out[dir+"/"+refPath] = []byte(content)
		}
	}
	return out
}

// audienceIncludesCoding reports whether audience names the coding agent; an
// empty list is every audience (frontmatterAudience's default, repeated for
// a Skill built without it).
func audienceIncludesCoding(audience []string) bool {
	if len(audience) == 0 {
		return true
	}
	for _, a := range audience {
		if a == AudienceCoding {
			return true
		}
	}
	return false
}
