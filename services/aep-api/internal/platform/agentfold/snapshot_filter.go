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

package agentfold

// snapshot_filter.go — the turn-snapshot view filter, mirroring the agents
// service's load-workspace.ts (keepInTurnSnapshot + the walk's skip rules).
// The Phase 4b BaseReader MUST apply this filter so the Go fold sees exactly
// the base the agents-side FileBundle was seeded with: a path the agents walk
// dropped must read as exists=false here, or an editFile against it succeeds
// on one side only and the folds diverge.

import (
	"bytes"
	"path"
	"strings"
)

// The two OpenAPI contract shapes admitted into a turn snapshot alongside the
// agent-authored sources below: the produced contract
// (specs/design/components/<c>/openapi.yaml) and a consumed contract
// (specs/design/components/<c>/dependencies/<dep>.openapi.yaml). A
// resolution/collab turn must be able to read back a spec it (or a prior turn)
// just stored, so these two are admitted even though snapshots otherwise drop
// *.yaml (e.g. workload.yaml stays excluded). Mirrors the TS side's
// `[^/]*`-anchored regexes — path.Match's `*` matches within one path segment
// only, never crossing a `/`.
const (
	producedSpecPattern = "specs/design/components/*/openapi.yaml"
	consumedSpecPattern = "specs/design/components/*/dependencies/*.openapi.yaml"
)

func isAdmittedSpecPath(p string) bool {
	if ok, _ := path.Match(producedSpecPattern, p); ok {
		return true
	}
	ok, _ := path.Match(consumedSpecPattern, p)
	return ok
}

// KeepInTurnSnapshot mirrors keepInTurnSnapshot: keep agent-authored sources
// (*.md, *.dsl, *.cell, a design.json or validation-criteria.json basename,
// the two OpenAPI contract shapes above) and drop everything else. *.cell is
// the project-level cell-diagram DSL (design.cell). validation-criteria.json
// is kept so a design regeneration can see the existing acceptance oracle and
// reuse its criterion ids (keeping committed e2e specs, which are keyed by
// criterion id, mapped) instead of renumbering. Arbitrary *.yaml (e.g.
// workload.yaml) stays excluded — only the two exact shapes are admitted.
func KeepInTurnSnapshot(path string) bool {
	if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".dsl") || strings.HasSuffix(path, ".cell") {
		return true
	}
	if isAdmittedSpecPath(path) {
		return true
	}
	if isTextReferencePath(path) {
		return true
	}
	base := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		base = path[i+1:]
	}
	return base == "design.json" || base == "validation-criteria.json"
}

// referencesPrefix is where a user-uploaded reference appears inside a turn's
// snapshot. Not a repo path — nothing is committed there (console ADR-0017);
// the engine overlays the off-git store into the extracted snapshot at this
// prefix.
const referencesPrefix = "specs/requirements/references/"

// nativeReferenceExts are the reference types the model reads NATIVELY, as file
// parts rather than as text. Mirrors NATIVE_MEDIA_BY_EXT in the agents service.
var nativeReferenceExts = map[string]bool{
	".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// isTextReferencePath mirrors isTextReferencePath in the agents service: a
// user-uploaded reference the model should read as TEXT.
//
// References are the one input here the agent did not author, so the extension
// allow-list above — built for agent-authored spec artifacts — is the wrong
// test for them: it admits a .md reference and silently drops a .txt or .csv
// one, which then reaches the turn as a file on disk that nothing puts in front
// of the model. The rule is the folder, not the extension.
//
// Natively-read binaries are excluded deliberately: they ride as file parts,
// and admitting them here would pour a PDF's bytes into the text map — the
// failure that channel exists to avoid.
func isTextReferencePath(p string) bool {
	if !strings.HasPrefix(p, referencesPrefix) {
		return false
	}
	dot := strings.LastIndexByte(p, '.')
	if dot < 0 {
		return true
	}
	return !nativeReferenceExts[strings.ToLower(p[dot:])]
}

// InTurnSnapshot is the complete per-file predicate the agents-side snapshot
// walk applies: dot-led path segments skipped at any depth, the keep-filter,
// and the NUL-byte binary skip. Phase 4b BaseReader implementations should
// return exists=false for any file failing this.
func InTurnSnapshot(path string, content []byte) bool {
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	if !KeepInTurnSnapshot(path) {
		return false
	}
	return !bytes.ContainsRune(content, 0)
}
