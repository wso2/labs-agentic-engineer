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

// Package agentfold is the Go half of the D14 fold-parity contract: it
// re-implements the
// TS FileBundle fold (packages/agent-stream/src/bundle.ts + change.ts) so
// aep-api can reconstruct a turn's file mutations from the agent SSE stream
// and commit them, gated by the terminal integrity manifest. Every op
// reproduces the TS semantics exactly — literal substring matching, CRLF→LF
// canonicalization at the same points, idempotent already-applied/noop
// results, the YAML reparse guard and the component design.json schema gate
// (an exact port of the agent's zod gate — see designgate.go for why
// internal/platform/designspec could NOT be reused), and the npm-yaml-parity
// frontmatter re-stringify (yamlemit.go).
//
// Gates that exist only on the TS side (openapi.yaml, security.json, the
// design diagrams) are NOT ported: the fold applies a mutation only once the
// stream's own verdict for it says the TS bundle accepted it (ADR-0021), so a
// TS-side rejection is never applied here and parity holds for every gate,
// present and future.
//
// Parity is locked by cassette-replay goldens (fold_golden_test.go): every
// recorded stream folded here must byte-equal the TS fold's committed golden.
// Where byte parity cannot be guaranteed (frontmatter shapes outside the
// ported yaml-stringify subset) the fold fails LOUDLY with a typed error —
// under committed-truth generation a failed turn is safe, a divergent commit
// is not.
package agentfold

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// BaseReader supplies the turn's base file content on demand (lazy seed).
//
// Contract for the Phase 4b caller (the turn runner): the reader MUST present
// the same view of the base ref that the agents service read from the
// snapshot — i.e. only paths accepted by InTurnSnapshot (the *.md / *.dsl /
// design.json keep-filter, no dot-led path segments, no NUL-bytes binary
// content; see snapshot_filter.go). A path outside that view must return
// exists=false even if it is present in git: the TS FileBundle never saw it,
// so ops against it must fail NO_SUCH_FILE here too or the folds diverge.
// Content is returned raw; the fold applies CRLF→LF canonicalization itself
// (the TS bundle constructor does the same to its seed).
//
// The golden tests back this with the recorded cassette's request.body.files
// map — the exact (FE-filtered) snapshot the TS fold was seeded with.
type BaseReader func(ctx context.Context, path string) (content []byte, exists bool, err error)

// Op names the canonical file mutations (the TS `Op` union).
type Op string

const (
	OpAdd    Op = "add"
	OpEdit   Op = "edit"
	OpRemove Op = "remove"
)

// OpStatus is the success detail: applied = state changed;
// already-applied / noop = idempotent no-change.
type OpStatus string

const (
	StatusApplied        OpStatus = "applied"
	StatusAlreadyApplied OpStatus = "already-applied"
	StatusNoop           OpStatus = "noop"
)

// ErrCode mirrors the TS ErrCode union.
type ErrCode string

const (
	ErrAlreadyExists   ErrCode = "ALREADY_EXISTS"
	ErrInvalidPath     ErrCode = "INVALID_PATH"
	ErrNotFound        ErrCode = "NOT_FOUND"
	ErrNotUnique       ErrCode = "NOT_UNIQUE"
	ErrNoSuchFile      ErrCode = "NO_SUCH_FILE"
	ErrEmptyOldString  ErrCode = "EMPTY_OLD_STRING"
	ErrInvalidYAML     ErrCode = "INVALID_YAML"
	ErrInvalidJSON     ErrCode = "INVALID_JSON"
	ErrSchemaViolation ErrCode = "SCHEMA_VIOLATION"
	ErrInvalidDSL      ErrCode = "INVALID_DSL"
	ErrProtectedPath   ErrCode = "PROTECTED_PATH"
	// Gates that live ONLY on the TS side (openapi.yaml, security.json, the
	// design diagrams). The fold never re-judges them: it applies a write
	// only once the stream's own verdict says the TS gate accepted it.
	ErrInvalidOpenAPI     ErrCode = "INVALID_OPENAPI"
	ErrInvalidDiagram     ErrCode = "INVALID_DIAGRAM"
	ErrUnknownParticipant ErrCode = "UNKNOWN_PARTICIPANT"
	ErrUnknownDependency  ErrCode = "UNKNOWN_DEPENDENCY"
)

// MatchCandidate echoes a source line for NOT_UNIQUE / NOT_FOUND re-anchoring.
type MatchCandidate struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// OpResult is the outcome of one canonical op (the TS OpOk | OpErr union,
// flattened). It never carries file content.
type OpResult struct {
	OK   bool   `json:"ok"`
	Path string `json:"path"`
	Op   Op     `json:"op"`
	// Success detail (OK).
	Status OpStatus `json:"status,omitempty"`
	// Failure detail (!OK).
	Code       ErrCode          `json:"code,omitempty"`
	Message    string           `json:"message,omitempty"`
	Candidates []MatchCandidate `json:"candidates,omitempty"`
	Count      int              `json:"count,omitempty"`
}

// protectedPaths are the structural roots removeFile refuses to delete.
var protectedPaths = map[string]bool{
	"specs/requirements/prd.md": true,
	"specs/design/design.cell":  true,
}

const maxCandidates = 6

// Fold accumulates one turn's file mutations over a lazily-read base.
// Overlay semantics: a touched path shadows the base (nil = deleted); reads
// consult overlay, then the base-read cache, then the BaseReader. Not safe
// for concurrent use — one fold per turn, driven by the stream tap.
type Fold struct {
	base      BaseReader
	baseCache map[string]*string // lf-canonical; nil = known-absent
	overlay   map[string]*string // touched paths only; nil = deleted
	// pending holds mutation tool-calls awaiting the stream's verdict for
	// them (ApplyToolCall): applied on ok:true, dropped otherwise.
	pending map[string]StreamPart
}

// New builds a Fold over a lazy BaseReader (Phase 4b: Workspace.ReadFile at
// the turn's base ref, filtered per the BaseReader contract).
func New(base BaseReader) *Fold {
	return &Fold{
		base:      base,
		baseCache: map[string]*string{},
		overlay:   map[string]*string{},
	}
}

// read returns the current LF-canonical content of path (overlay over base).
func (f *Fold) read(ctx context.Context, path string) (string, bool, error) {
	if v, ok := f.overlay[path]; ok {
		if v == nil {
			return "", false, nil
		}
		return *v, true, nil
	}
	if c, ok := f.baseCache[path]; ok {
		if c == nil {
			return "", false, nil
		}
		return *c, true, nil
	}
	raw, exists, err := f.base(ctx, path)
	if err != nil {
		return "", false, fmt.Errorf("agentfold: read base %q: %w", path, err)
	}
	if !exists {
		f.baseCache[path] = nil
		return "", false, nil
	}
	// Node reads snapshots via Buffer.toString("utf8"), which substitutes
	// U+FFFD for invalid sequences; mirror that before canonicalizing.
	content := lf(strings.ToValidUTF8(string(raw), "�"))
	f.baseCache[path] = &content
	return content, true, nil
}

// AddFile is FileBundle.addFile.
func (f *Fold) AddFile(ctx context.Context, path, content string) (OpResult, error) {
	const op = OpAdd
	if jsTrim(path) == "" {
		return opErr(path, op, ErrInvalidPath, "path must be a non-empty string."), nil
	}
	next := lf(content)
	cur, exists, err := f.read(ctx, path)
	if err != nil {
		return OpResult{}, err
	}
	if exists {
		if cur == next {
			return opOk(path, op, StatusNoop), nil // identical re-add
		}
		return opErr(path, op, ErrAlreadyExists,
			path+" already exists — use editFile to change it, or removeFile then addFile to replace it wholesale."), nil
	}
	return f.commit(path, op, next, func(e string) string {
		return path + " would not be valid YAML: " + e
	}), nil
}

// EditFile is FileBundle.editFile: an anchored, exactly-once literal
// search/replace.
func (f *Fold) EditFile(ctx context.Context, path, oldString, newString string) (OpResult, error) {
	const op = OpEdit
	content, exists, err := f.read(ctx, path)
	if err != nil {
		return OpResult{}, err
	}
	if !exists {
		// Deviation from TS: the lazy base cannot enumerate, so the message
		// omits the "Available: …" listing (log-only text; state parity holds).
		return opErr(path, op, ErrNoSuchFile, path+" is not in the bundle."), nil
	}
	if oldString == "" {
		return opErr(path, op, ErrEmptyOldString, "oldString must be non-empty; to create a file use addFile."), nil
	}
	oldS := lf(oldString)
	newS := lf(newString)

	starts := occurrences(content, oldS)
	if len(starts) == 0 {
		// Idempotency: already-applied only when newString is present at a
		// UNIQUE site (a loose substring test would mask a genuine NOT_FOUND).
		if jsTrim(newS) != "" && len(occurrences(content, newS)) == 1 {
			return opOk(path, op, StatusAlreadyApplied), nil
		}
		r := opErr(path, op, ErrNotFound,
			"oldString did not match any text in "+path+". Copy the snippet verbatim, including leading indentation and newlines.")
		r.Candidates = closestLines(content, oldS)
		return r, nil
	}
	if len(starts) > 1 {
		r := opErr(path, op, ErrNotUnique,
			fmt.Sprintf("oldString matched %d locations in %s. Broaden it with surrounding lines (e.g. the parent YAML key or the preceding heading) until it is unique.", len(starts), path))
		n := len(starts)
		if n > maxCandidates {
			n = maxCandidates
		}
		for _, idx := range starts[:n] {
			r.Candidates = append(r.Candidates, lineCandidate(content, idx))
		}
		r.Count = len(starts)
		return r, nil
	}

	idx := starts[0]
	after := content[:idx] + newS + content[idx+len(oldS):]
	return f.commit(path, op, after, func(e string) string {
		return "Edit rejected — result would not be valid YAML: " + e + ". The file is unchanged; fix the indentation of newString and retry."
	}), nil
}

// RemoveFile is FileBundle.removeFile (idempotent; structural roots refused).
func (f *Fold) RemoveFile(ctx context.Context, path string) (OpResult, error) {
	const op = OpRemove
	if protectedPaths[path] {
		return opErr(path, op, ErrProtectedPath, path+" is a structural root and cannot be deleted."), nil
	}
	_, exists, err := f.read(ctx, path)
	if err != nil {
		return OpResult{}, err
	}
	if !exists {
		return opOk(path, op, StatusNoop), nil // idempotent delete
	}
	f.overlay[path] = nil
	return opOk(path, op, StatusApplied), nil
}

// commit applies content to path gated by the YAML reparse guard, the
// component design.json schema gate, and the wireframes .dsl syntax gate;
// a rejection leaves the fold byte-for-byte unchanged.
func (f *Fold) commit(path string, op Op, content string, rejectMsg func(yamlErr string) string) OpResult {
	if yamlErr := checkYAMLGuard(path, content); yamlErr != "" {
		return opErr(path, op, ErrInvalidYAML, rejectMsg(yamlErr))
	}
	if code, msg := checkComponentDesignGuard(path, content); code != "" {
		return opErr(path, op, code, msg)
	}
	if code, msg := checkWireframeDslGuard(path, content); code != "" {
		return opErr(path, op, code, msg)
	}
	c := content
	f.overlay[path] = &c
	return opOk(path, op, StatusApplied)
}

// Touched returns the final state of every path mutated this turn:
// nil = deleted, else the LF-canonical final content. (The TS
// bundle.touched() set, with contents attached.)
func (f *Fold) Touched() map[string]*string {
	out := make(map[string]*string, len(f.overlay))
	for p, v := range f.overlay {
		out[p] = v
	}
	return out
}

// FullState overlays the fold's mutations onto a full seed snapshot —
// the TS bundle.snapshot() equivalent, for golden comparison. The seed is
// LF-canonicalized exactly like the FileBundle constructor.
//
//deadcode:keep test seam — full-snapshot readback for fold golden tests, incl. delivery/execution component test (cross-package).
func (f *Fold) FullState(seed map[string]string) map[string]string {
	out := make(map[string]string, len(seed))
	for p, c := range seed {
		out[p] = lf(c)
	}
	for p, v := range f.overlay {
		if v == nil {
			delete(out, p)
		} else {
			out[p] = *v
		}
	}
	return out
}

// ---- stream-driven application ---------------------------------------------

// opByTool mirrors OP_BY_TOOL (change.ts).
var opByTool = map[string]Op{
	"addFile":    OpAdd,
	"editFile":   OpEdit,
	"removeFile": OpRemove,
}

// IsFileMutationTool reports whether toolName is one of the canonical
// file-mutation tools (change.ts isFileMutationTool).
func IsFileMutationTool(toolName string) bool {
	_, ok := opByTool[toolName]
	return ok
}

// ApplyToolCall folds ONE parsed StreamPart into the fold. A mutation
// `tool-call` is held until the stream's own verdict for it arrives — the
// `tool-result` the agents-side write ledger emits right behind it, carrying
// the TS FileBundle's OpResult — and applied only when that verdict is
// ok:true (ADR-0021). A rejected write is dropped unapplied, so a gate that
// exists only in TS (openapi, security.json, the design diagrams) can never
// put the fold ahead of the bundle: parity is by construction, not by
// porting every gate. Everything else — other frame types, unknown tools,
// malformed inputs, a verdict for a call the fold never saw — is silently
// ignored (nil, nil). The result is returned for logging; state parity does
// not depend on it.
//
// A tool-call with no toolCallId cannot be paired with a verdict and applies
// at once — the pre-ledger shape, kept for the driving tests.
func (f *Fold) ApplyToolCall(ctx context.Context, part StreamPart) (*OpResult, error) {
	switch part.Type {
	case "tool-call":
		if !IsFileMutationTool(part.ToolName) {
			return nil, nil
		}
		if part.ToolCallID != "" {
			if f.pending == nil {
				f.pending = map[string]StreamPart{}
			}
			f.pending[part.ToolCallID] = part
			return nil, nil
		}
		return f.applyCall(ctx, part)
	case "tool-result":
		call, ok := f.pending[part.ToolCallID]
		if !ok {
			return nil, nil
		}
		delete(f.pending, part.ToolCallID)
		if !verdictAccepted(part.Output) {
			return nil, nil
		}
		return f.applyCall(ctx, call)
	default:
		return nil, nil
	}
}

// verdictAccepted reads the `ok` of the OpResult a tool-result carries. Only
// an explicit true applies: an absent or malformed verdict drops the write,
// and the manifest gate then rejects the turn loudly rather than the fold
// guessing — under committed-truth generation a failed turn is safe, a
// divergent commit is not.
func verdictAccepted(output json.RawMessage) bool {
	var v struct {
		OK *bool `json:"ok"`
	}
	if err := json.Unmarshal(output, &v); err != nil || v.OK == nil {
		return false
	}
	return *v.OK
}

// applyCall runs one accepted mutation tool-call through the fold's ops.
func (f *Fold) applyCall(ctx context.Context, part StreamPart) (*OpResult, error) {
	in, ok := decodeToolInput(part.Input)
	if !ok {
		return nil, nil
	}
	var (
		res OpResult
		err error
	)
	switch part.ToolName {
	case "addFile":
		path, okP := asString(in["path"])
		content, okC := asString(in["content"])
		if !okP || !okC {
			return nil, nil
		}
		res, err = f.AddFile(ctx, path, content)
	case "editFile":
		path, okP := asString(in["path"])
		oldS, okO := asString(in["oldString"])
		newS, okN := asString(in["newString"])
		if !okP || !okO || !okN {
			return nil, nil
		}
		res, err = f.EditFile(ctx, path, oldS, newS)
	case "removeFile":
		path, okP := asString(in["path"])
		if !okP {
			return nil, nil
		}
		res, err = f.RemoveFile(ctx, path)
	}
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// decodeToolInput parses a tool-call `input` payload. Numbers decode as
// json.Number so out-of-range literals can round to ±Inf like JS instead of
// failing the whole decode.
func decodeToolInput(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// ---- helpers (bundle.ts ports) ------------------------------------------------

// lf is the canonical newline form — all fold content is stored LF.
func lf(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// u16Len is the UTF-16 code-unit length (JS String.prototype.length).
func u16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsTrim is String.prototype.trim: ECMA WhiteSpace (TAB VT FF SP NBSP ZWNBSP
// + Unicode Zs) plus LineTerminator (LF CR LS PS). Notably NEL (U+0085) is
// NOT trimmed, unlike Go's unicode.IsSpace.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '\t', '\v', '\f', ' ', 0x00a0, 0xfeff, '\n', '\r', 0x2028, 0x2029:
			return true
		}
		return r == 0x1680 || (r >= 0x2000 && r <= 0x200a) || r == 0x202f || r == 0x205f || r == 0x3000
	})
}

func opOk(path string, op Op, status OpStatus) OpResult {
	return OpResult{OK: true, Path: path, Op: op, Status: status}
}

func opErr(path string, op Op, code ErrCode, message string) OpResult {
	return OpResult{Path: path, Op: op, Code: code, Message: message}
}

// occurrences returns the non-overlapping byte start indices of needle.
func occurrences(haystack, needle string) []int {
	var out []int
	pos := 0
	for {
		i := strings.Index(haystack[pos:], needle)
		if i < 0 {
			return out
		}
		out = append(out, pos+i)
		pos += i + len(needle)
	}
}

// lineNumberAt is the 1-based line number of the byte at index.
func lineNumberAt(content string, index int) int {
	return 1 + strings.Count(content[:index], "\n")
}

func lineTextAt(content string, lineNumber int) string {
	lines := strings.Split(content, "\n")
	if lineNumber-1 < len(lines) {
		return lines[lineNumber-1]
	}
	return ""
}

// lineCandidate is the line a match starts on, for the NOT_UNIQUE echo.
func lineCandidate(content string, startIdx int) MatchCandidate {
	line := lineNumberAt(content, startIdx)
	return MatchCandidate{Line: line, Text: jsTrim(lineTextAt(content, line))}
}

// closestLines is the best-effort "did you mean" for NOT_FOUND: lines sharing
// the first 16 UTF-16 units of the anchor's first non-empty line.
func closestLines(content, needle string) []MatchCandidate {
	firstLine := ""
	for _, l := range strings.Split(needle, "\n") {
		if jsTrim(l) != "" {
			firstLine = jsTrim(l)
			break
		}
	}
	probe := u16Slice(firstLine, 16)
	if u16Len(probe) < 4 {
		return nil
	}
	var out []MatchCandidate
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines) && len(out) < 2; i++ {
		if strings.Contains(lines[i], probe) {
			out = append(out, MatchCandidate{Line: i + 1, Text: jsTrim(lines[i])})
		}
	}
	return out
}

// u16Slice is String.prototype.slice(0, n) in UTF-16 units.
func u16Slice(s string, n int) string {
	if u16Len(s) <= n {
		return s
	}
	units := 0
	for i, r := range s {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
		if units == n {
			// May split a surrogate pair exactly at n; JS would keep the lone
			// high surrogate. Unrepresentable in Go — truncate at the rune.
			return s[:i+len(string(r))]
		}
	}
	return s
}

// checkComponentDesignGuard mirrors checkComponentDesign: authored component
// design.json is schema-gated on every write. Reuses the platform designspec
// validator (same accept/reject behavior as the agent's zod gate; messages
// differ, which is log-only).
func checkComponentDesignGuard(path, content string) (ErrCode, string) {
	m := componentDesignRe.FindStringSubmatch(path)
	if m == nil {
		return "", ""
	}
	err := validateComponentDesign(content, m[1])
	if err == nil {
		return "", ""
	}
	return err.code, path + ": " + err.message
}
