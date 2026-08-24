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

// The project descriptor: `specs/.agentic-engineer.toml`. It does two jobs at
// once — it MARKS a repo as an Agentic Engineer project, and it carries the
// idea the user typed when they created it, which the `/start` flow needs to
// generate requirements from.
//
// The descriptor is deliberately invisible to the agent. Every dot-led path
// segment is skipped by the turn-snapshot walk (agentfold.InTurnSnapshot, and
// its TS mirror in services/agents load-workspace.ts), and `.toml` is not an
// admitted extension in KeepInTurnSnapshot either — so the model can never
// read this file even by asking. The idea reaches a turn ONLY through the
// server-side steering append (ideaSteer, wired in genai_service). That is why
// there is no "read the descriptor" tool and no instruction telling the agent
// where the file lives.
//
// It is equally invisible in the console's Spec view: toSpecEntry keeps only
// `specs/<requirements|design|validation>/<file>`, and this path has too few
// segments to qualify (#113 decision 3).
//
// TOML — not the YAML/JSON used elsewhere — because the one field that matters
// is a paragraph of free text a user typed. A real encoder is used rather than
// hand-rolled key writing precisely so quotes, backslashes and newlines in that
// text round-trip instead of corrupting the file.

package spec

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// DescriptorPath is the descriptor's fixed repo-relative path. It sits at the
// specs/ ROOT (not under requirements/) so it is never mistaken for a
// requirements document by the versioned-artifact bundle.
const DescriptorPath = "specs/.agentic-engineer.toml"

// DescriptorAPIVersion is stamped into every descriptor this platform writes.
// Its presence is what identifies the file as ours.
const DescriptorAPIVersion = "agentic-engineer/v1"

// Descriptor is the parsed descriptor. Identity fields are informational; Idea
// is the load-bearing one — the user's own words, captured once at creation.
type Descriptor struct {
	APIVersion string `toml:"apiVersion"`
	Name       string `toml:"name"`
	CreatedAt  string `toml:"createdAt"`
	Idea       string `toml:"idea"`
}

// NewDescriptor builds a descriptor with the current apiVersion stamped, so no
// caller has to remember to set it.
func NewDescriptor(name, idea, createdAt string) Descriptor {
	return Descriptor{
		APIVersion: DescriptorAPIVersion,
		Name:       name,
		CreatedAt:  createdAt,
		Idea:       strings.TrimSpace(idea),
	}
}

// MarshalDescriptor renders a descriptor to TOML bytes.
func MarshalDescriptor(d Descriptor) ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(d); err != nil {
		return nil, fmt.Errorf("encode descriptor: %w", err)
	}
	return buf.Bytes(), nil
}

// ParseDescriptor reads descriptor TOML. A malformed file is an error, never a
// silently-empty descriptor — callers that want best-effort behavior (the
// steering read) decide that for themselves.
func ParseDescriptor(raw []byte) (Descriptor, error) {
	var d Descriptor
	if _, err := toml.Decode(string(raw), &d); err != nil {
		return Descriptor{}, fmt.Errorf("decode descriptor: %w", err)
	}
	return d, nil
}

// DescriptorWriter stamps the descriptor into a project repo. It writes through
// the ordinary Files API apply path — the descriptor lives under specs/, so it
// needs no gate widening (validatePath already admits it, dot-prefix and all).
type DescriptorWriter struct {
	files FilesService
	now   func() time.Time
}

// NewDescriptorWriter wires the writer over the Files service.
func NewDescriptorWriter(files FilesService) *DescriptorWriter {
	return &DescriptorWriter{files: files, now: time.Now}
}

// SpecIgnorePath is the ignore file scaffolded beside the descriptor, and
// SpecIgnoreContent is what it holds. It lives UNDER specs/ rather than at the
// repo root for one practical reason: the Files API's write scope is specs/-only
// (validatePath), so a root .gitignore would need a gate widening, while
// patterns in specs/.gitignore are already relative to specs/ and say the same
// thing. Dot-prefixed like the descriptor, so the same rule keeps it invisible
// to the agent.
//
// What it guards: reference documents are overlaid into the turn's snapshot at
// specs/requirements/references/ (gitfs.ReferenceOverlayDir) and must never be
// committed back from there. This is the guard that covers the coding-agent
// runner, which clones for real and stages with git — a path no server-side
// predicate sees. The collab committer's own reference predicate is NOT made
// redundant by it: that committer builds writes from the room, not a working
// tree, so .gitignore does not apply to it at all.
const (
	SpecIgnorePath    = "specs/.gitignore"
	SpecIgnoreContent = "# Reference documents are transient turn inputs, not spec artifacts.\n" +
		"# The platform overlays them into each turn's workspace; they are never committed.\n" +
		"requirements/references/\n"
)

// WriteDescriptor commits specs/.agentic-engineer.toml for a freshly-created
// project, plus the ignore file above. An empty idea is written as an empty
// field rather than skipped: the file's other job is to MARK the repo as an
// Agentic Engineer project.
//
// One apply, so a new repo is never left marked-but-unguarded (or the reverse)
// by a failure between two commits.
func (w *DescriptorWriter) WriteDescriptor(ctx context.Context, orgID, projectID, name, idea string) error {
	if w == nil || w.files == nil {
		return nil
	}
	raw, err := MarshalDescriptor(NewDescriptor(name, idea, w.now().UTC().Format(time.RFC3339)))
	if err != nil {
		return err
	}
	_, _, err = w.files.Apply(ctx, orgID, projectID, ApplyRequest{
		Writes: []WriteOp{
			{Path: DescriptorPath, Content: string(raw)},
			{Path: SpecIgnorePath, Content: SpecIgnoreContent},
		},
		Message: "chore: initialize the agentic-engineer project descriptor",
	})
	return err
}

// readProjectIdea reads the captured idea at `at`, best-effort: a project with
// no descriptor, an unreadable one, or a corrupt one yields "" and the turn
// proceeds without it. Deliberately never an error — losing the idea costs the
// user one extra question from the start skill, whereas failing the turn costs
// them their kickoff.
//
// The exact-path predicate is what makes this work at all: ReadBundle applies
// ONLY the caller's filter (gitfs lsTree lists dot-entries like any other), so
// the descriptor is readable here even though the turn-snapshot walk that
// builds the agent's view drops it.
func (s *Service) readProjectIdea(ctx context.Context, ref sourcecontrol.RepoRef, at string) string {
	files, _, err := s.git.Workspace().ReadBundle(ctx, ref, at, func(rel string) bool {
		return rel == DescriptorPath
	})
	if err != nil {
		slog.WarnContext(ctx, "descriptor unreadable; turn continues without the captured idea",
			"path", DescriptorPath, "error", err)
		return ""
	}
	raw := files[DescriptorPath]
	if strings.TrimSpace(raw) == "" {
		return "" // no descriptor: an older project, or a best-effort write that failed
	}
	d, err := ParseDescriptor([]byte(raw))
	if err != nil {
		slog.WarnContext(ctx, "descriptor malformed; turn continues without the captured idea",
			"path", DescriptorPath, "error", err)
		return ""
	}
	return d.Idea
}
