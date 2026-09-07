/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * FileBundle — the in-memory spec bundle the main agent mutates for one turn.
 *
 * The whole point of this module is LATENCY: the files are already inlined in
 * the agent's prompt (free, prefill-cached), so re-emitting a file body to
 * change two lines is the expensive thing. `editFile` is an anchored
 * search/replace whose output cost scales with the EDIT, not the file. See
 * services/agents/design/ADR-0001-anchored-file-edits.md for the full rationale.
 *
 * Design invariants this class enforces (pure, no I/O, fully testable):
 *  - Matching is LITERAL substring with NO structural normalization, so
 *    indentation in YAML / frontmatter is load-bearing and preserved byte for
 *    byte. The only normalization is CRLF -> LF (newlines are not meaningful
 *    in this corpus). All content is stored LF-canonical.
 *  - An edit must match EXACTLY ONCE; ambiguity returns the line number + text
 *    of every candidate so the model re-anchors in one corrective step.
 *  - Every op is idempotent: a repeated/duplicated call returns an
 *    `already-applied` / `noop` SUCCESS, never a loop-wedging hard error.
 *  - Writes to YAML / frontmatter files run a parse-only reparse guard; on
 *    failure the bundle is left byte-for-byte unchanged (reject, don't corrupt).
 */

import { parse as parseYaml } from "yaml";
import { checkComponentDesign } from "./component-design-schema.js";
import { checkSecurityDesign } from "./security-design-schema.js";
import { checkOpenapiSpec } from "./openapi-spec.js";
import { checkWireframeLayout } from "./wireframe-layout.js";
import { checkDesignDiagram } from "./design-diagrams.js";
import { checkComponentDependencies } from "./component-dependencies.js";
import type {
  Op,
  ErrCode,
  MatchCandidate,
  OpOk,
  OpErr,
  OpResult,
} from "./contracts/sse-events.js";

// The op shapes & result types are the WIRE contract — defined once in
// `src/contracts/sse-events.ts` and imported here type-only (erased at runtime, so the
// domain stays dependency-light). Re-exported so in-package consumers
// (`tool.ts`, the tests) keep importing them from the domain module.
// NOTE: a successful `OpOk` carries NO file content (no `newContent`) — see §5
// of the design doc / ADR-0001; the model anchors from its own tool-call args
// and the re-inlined CURRENT STATE, and consumers reconstruct state from the
// stream (`applyToolCall`).
export type { Op, ErrCode, MatchCandidate, OpOk, OpErr, OpResult };

/** Structural root files the demo refuses to delete. */
const PROTECTED_PATHS = new Set<string>([
  "specs/requirements/prd.md",
  "specs/design/design.cell",
]);

/** Leading YAML frontmatter fence: `---\n<block>\n---`. */
export const FRONTMATTER_RE = /^---\n([\s\S]*?)\n---\n?/;

const MAX_CANDIDATES = 6;

export class FileBundle {
  private files = new Map<string, string>();
  private touchedPaths = new Set<string>();

  constructor(initial: Record<string, string> = {}) {
    for (const [path, content] of Object.entries(initial)) {
      this.files.set(path, lf(content));
    }
  }

  // -- Reads --------------------------------------------------------------

  has(path: string): boolean {
    return this.files.has(path);
  }

  read(path: string): string | undefined {
    return this.files.get(path);
  }

  list(): string[] {
    return [...this.files.keys()].sort();
  }

  snapshot(): Record<string, string> {
    return Object.fromEntries(this.files);
  }

  touched(): string[] {
    return [...this.touchedPaths];
  }

  // -- Ops ----------------------------------------------------------------

  addFile(path: string, content: string): OpResult {
    const op: Op = "add";
    if (!path.trim()) {
      return err(path, op, "INVALID_PATH", "path must be a non-empty string.");
    }
    const next = lf(content);
    if (this.files.has(path)) {
      if (this.files.get(path) === next) {
        return ok(path, op, "noop"); // identical re-add
      }
      return err(
        path,
        op,
        "ALREADY_EXISTS",
        `${path} already exists — use editFile to change it, or removeFile then addFile to replace it wholesale.`,
      );
    }
    return this.commit(path, op, next, (e) => `${path} would not be valid YAML: ${e}`);
  }

  editFile(path: string, oldString: string, newString: string): OpResult {
    const op: Op = "edit";
    if (!this.files.has(path)) {
      return err(
        path,
        op,
        "NO_SUCH_FILE",
        `${path} is not in the bundle. Available: ${this.list().join(", ")}.`,
      );
    }
    if (oldString === "") {
      return err(path, op, "EMPTY_OLD_STRING", "oldString must be non-empty; to create a file use addFile.");
    }
    const content = this.files.get(path)!; // already LF
    const oldS = lf(oldString);
    const newS = lf(newString);

    const starts = occurrences(content, oldS);
    if (starts.length === 0) {
      // Idempotency: treat as already-applied only when newString is present at
      // a UNIQUE site. A loose substring test would mask a genuine NOT_FOUND
      // (e.g. a short newString that coincidentally occurs inside another word),
      // silently dropping a requested change — worse than a corrective retry.
      if (newS.trim() !== "" && occurrences(content, newS).length === 1) {
        return ok(path, op, "already-applied");
      }
      return err(
        path,
        op,
        "NOT_FOUND",
        `oldString did not match any text in ${path}. Copy the snippet verbatim, including leading indentation and newlines.`,
        closestLines(content, oldS),
      );
    }
    if (starts.length > 1) {
      return err(
        path,
        op,
        "NOT_UNIQUE",
        `oldString matched ${starts.length} locations in ${path}. Broaden it with surrounding lines (e.g. the parent YAML key or the preceding heading) until it is unique.`,
        starts.slice(0, MAX_CANDIDATES).map((idx) => lineCandidate(content, idx)),
        starts.length,
      );
    }

    const idx = starts[0]!;
    // slice-based splice avoids String.replace's `$`-pattern interpretation.
    const after = content.slice(0, idx) + newS + content.slice(idx + oldS.length);

    return this.commit(
      path,
      op,
      after,
      (e) =>
        `Edit rejected — result would not be valid YAML: ${e}. The file is unchanged; fix the indentation of newString and retry.`,
    );
  }

  removeFile(path: string): OpResult {
    const op: Op = "remove";
    if (PROTECTED_PATHS.has(path)) {
      return err(path, op, "PROTECTED_PATH", `${path} is a structural root and cannot be deleted.`);
    }
    if (!this.files.has(path)) {
      return ok(path, op, "noop"); // idempotent delete
    }
    this.files.delete(path);
    this.touchedPaths.add(path);
    return ok(path, op, "applied");
  }

  /**
   * Apply `content` to `path` through the write-gate ladder: YAML reparse, then
   * each artifact-specific gate that claims the path (component `design.json`
   * schema, `security.json` schema, `wireframes.dsl` syntax, `openapi.yaml`
   * structure, the design diagrams' shape and participants). The first
   * problem aborts with its own code and NO write, leaving the bundle
   * byte-for-byte unchanged — the safe in-memory contract. Every gate is a pure
   * (path, content) => problem | null function, so a new artifact kind is one
   * module and one call here.
   */
  private commit(path: string, op: Op, content: string, rejectMsg: (yamlErr: string) => string): OpResult {
    const yamlErr = checkYaml(path, content);
    if (yamlErr) {
      return err(path, op, "INVALID_YAML", rejectMsg(yamlErr));
    }
    // Authored component design.json is schema-gated the same way YAML is
    // reparse-gated: an invalid write aborts with a self-correctable error
    // and the bundle stays byte-for-byte unchanged.
    const jsonProblem = checkComponentDesign(path, content);
    if (jsonProblem) {
      return err(path, op, jsonProblem.code, jsonProblem.message);
    }
    // A component's dependencies are edges of the cell — the design's source
    // of truth — so a dependency the cell does not declare (a database found
    // during enrichment and never drawn) is refused while the agent can still
    // add the node.
    const dependencyProblem = checkComponentDependencies(path, content, this);
    if (dependencyProblem) {
      return err(path, op, dependencyProblem.code, dependencyProblem.message);
    }
    // The security document is gated on the same terms: it is the ONE spec file
    // the platform acts on deterministically at build time (creating directory
    // roles, test users, and the Thunder client), so a malformed one must be
    // caught while the model can still fix it, not when the build gate refuses
    // the tag.
    const securityProblem = checkSecurityDesign(path, content);
    if (securityProblem) {
      return err(path, op, securityProblem.code, securityProblem.message);
    }
    // Wireframes .dsl is layout-gated the same way: out-of-frame or
    // partially-overlapping elements abort the write with the coordinates the
    // model needs to fix them (the compiler would render them verbatim).
    const layoutProblem = checkWireframeLayout(path, content);
    if (layoutProblem) {
      return err(path, op, layoutProblem.code, layoutProblem.message);
    }
    // A component's openapi.yaml is structure-gated on the same terms, which is
    // what makes asking a separate tool to validate it unnecessary — that ask
    // cost a round trip plus a full re-emission of the document as tool input.
    const specProblem = checkOpenapiSpec(path, content);
    if (specProblem) {
      return err(path, op, specProblem.code, specProblem.message);
    }
    // The design's diagram documents are shape-gated on the same terms — one
    // diagram per file (ADR-0020) — and a flow's participants are resolved
    // against the cell and the PRD already in this bundle, so an invented
    // participant is caught while the model can still fix it.
    const diagramProblem = checkDesignDiagram(path, content, this);
    if (diagramProblem) {
      return err(path, op, diagramProblem.code, diagramProblem.message);
    }
    this.files.set(path, content);
    this.touchedPaths.add(path);
    return ok(path, op, "applied");
  }
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

/** Canonical newline form — all bundle content is stored LF. */
export function lf(s: string): string {
  return s.replace(/\r\n/g, "\n");
}

function ok(path: string, op: Op, status: OpOk["status"]): OpOk {
  return { ok: true, path, op, status };
}

function err(
  path: string,
  op: Op,
  code: ErrCode,
  message: string,
  candidates?: MatchCandidate[],
  count?: number,
): OpErr {
  const e: OpErr = { ok: false, path, op, code, message };
  if (candidates && candidates.length) e.candidates = candidates;
  if (count !== undefined) e.count = count;
  return e;
}

function msg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** Non-overlapping start indices of `needle` in `haystack`. */
function occurrences(haystack: string, needle: string): number[] {
  const out: number[] = [];
  let pos = 0;
  while (true) {
    const i = haystack.indexOf(needle, pos);
    if (i < 0) break;
    out.push(i);
    pos = i + needle.length;
  }
  return out;
}

/** 1-based line number of the character at `index`. */
function lineNumberAt(content: string, index: number): number {
  let line = 1;
  for (let i = 0; i < index; i++) {
    if (content[i] === "\n") line++;
  }
  return line;
}

function lineTextAt(content: string, lineNumber: number): string {
  return content.split("\n")[lineNumber - 1] ?? "";
}

/** The line a match starts on, for NOT_UNIQUE candidate echo. */
function lineCandidate(content: string, startIdx: number): MatchCandidate {
  const line = lineNumberAt(content, startIdx);
  return { line, text: lineTextAt(content, line).trim() };
}

/**
 * Best-effort "did you mean" for NOT_FOUND: file lines that share the first
 * meaningful token of the (failed) anchor's first non-empty line. Cheap, so
 * the model sees the whitespace/escape delta instead of re-copying blind.
 */
function closestLines(content: string, needle: string): MatchCandidate[] {
  const firstLine = needle.split("\n").find((l) => l.trim().length > 0)?.trim() ?? "";
  const probe = firstLine.slice(0, 16);
  if (probe.length < 4) return [];
  const out: MatchCandidate[] = [];
  const lines = content.split("\n");
  for (let i = 0; i < lines.length && out.length < 2; i++) {
    if (lines[i]!.includes(probe)) out.push({ line: i + 1, text: lines[i]!.trim() });
  }
  return out;
}

/**
 * Parse-only reparse guard. Returns an error string if the post-edit content
 * is not valid YAML (whole doc for *.yaml, fence block only for frontmatter
 * files), or null when there is nothing to check / it parses cleanly. NEVER
 * re-serializes, so it adds safety without serializer drift.
 */
function checkYaml(path: string, content: string): string | null {
  try {
    if (/\.ya?ml$/i.test(path)) {
      parseYaml(content);
      return null;
    }
    const m = FRONTMATTER_RE.exec(content);
    if (m) parseYaml(m[1]!);
    return null;
  } catch (e) {
    return msg(e);
  }
}
