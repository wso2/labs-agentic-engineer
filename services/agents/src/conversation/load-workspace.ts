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
 * Workspace-shape input loading (shared-workspace-volume, D4): the ONLY module
 * that reads the shared mount. All three readers take a directory that
 * `snapshot-path.ts` derived and stat-checked — nothing here touches untrusted
 * paths, and nothing here writes.
 *
 *  - `readSnapshot(dir)` walks an immutable per-SHA repo snapshot into the
 *    in-memory `files` map a turn runs against (adapted from the proven
 *    playground `threads.ts` walk — POSIX-relative keys, dot-entries and binary
 *    skipped),
 *    with the aep-api `genai.keepInSnapshot` filter mirrored on top so the turn
 *    input stays a pure function of the sha while derived artifacts stay out.
 *  - `loadSkillsFromSnapshot(dir)` scans the `_skills` snapshot's
 *    `skills/<kind>/<name>/SKILL.md` catalog (frontmatter only — bodies are NOT
 *    retained) and returns a `SkillSource` whose `loadSkill`/
 *    `loadSkillReference` read from disk ON DEMAND: progressive disclosure is
 *    truly lazy, and D4 immutability makes the mid-turn reads race-free. It
 *    also reads the sidecar `<dir>/skills-manifest.json` once (ADR-0014) to
 *    drop any skill an org admin has DISABLED — such a skill never becomes a
 *    row, so it is absent from the catalog and `load`/`loadReference` return
 *    `undefined` for it, same as an unknown name.
 *  - `readReferenceAttachments(dir, references)` reads the natively-readable
 *    binary entries (`.pdf` and the four image types the models read natively —
 *    `.png`, `.jpg`/`.jpeg`, `.gif`, `.webp`) of a `start` turn's
 *    `TurnSpec.references` as native AI SDK file parts (#384).
 *    None of them is ever in the text `files` map — `keepInTurnSnapshot` admits
 *    none of those extensions, whatever the bytes look like, and the walk's
 *    NUL-byte skip is a second line behind that — so the two channels cannot
 *    double up on one document. Without this reader the model's only way to see
 *    a PDF was
 *    pulling it through a tool as "text", which is how a real turn died (an
 *    868KB PDF read as ~1.5M junk tokens, then the turn failed at history
 *    persistence on the NUL bytes that trip carried). Every failure mode here
 *    is best-effort: a missing/unreadable/oversized file is warned and skipped,
 *    never thrown — same posture as the skill readers above for ENOENT.
 */

import { existsSync, readFileSync, readdirSync, realpathSync, statSync, type Dirent } from "node:fs";
import { basename, join, resolve, sep } from "node:path";
import type { FilePart } from "ai";
import type { TurnAttachment } from "@aep/agent-stream";
import { parse as parseYaml } from "yaml";
// Reuse the bundle's single frontmatter grammar + LF canonicalizer so SKILL.md
// fence parsing cannot drift from the spec-file fence parsing (same approach as
// the caller-side skill resolver the playground uses to materialize the mount).
import { FRONTMATTER_RE, lf } from "@aep/agent-stream";
import type { SkillAudience, SkillLoadResult } from "../agents/main/skill-source.js";
import { ALL_AUDIENCES, SERVICE_AUDIENCE } from "../agents/main/skill-source.js";
import {
  SkillReadError,
  type SkillCatalogEntry,
  type SkillSource,
  type LoadedReference,
} from "../agents/main/skill-source.js";

export { SkillReadError };

/** Skill-read logger: ENOENT is expected (missing/vanished); other I/O is not. */
type SkillReadLog = (msg: string, err: unknown) => void;

const defaultSkillReadLog: SkillReadLog = (msg, err) => {
  const detail = err instanceof Error ? err.message : String(err);
  console.warn(`[skills] ${msg}: ${detail}`);
};

function isEnoent(err: unknown): boolean {
  return typeof err === "object" && err !== null && "code" in err && (err as { code: unknown }).code === "ENOENT";
}

/** Read text; return undefined on ENOENT. Log and throw SkillReadError on other I/O. */
function readSkillText(abs: string, log: SkillReadLog): string | undefined {
  try {
    return readFileSync(abs, "utf8");
  } catch (err) {
    if (isEnoent(err)) return undefined;
    log(`read failed ${abs}`, err);
    throw new SkillReadError(abs, err);
  }
}

/** Read bytes; return undefined on ENOENT. Log and throw SkillReadError on other I/O. */
function readSkillBytes(abs: string, log: SkillReadLog): Buffer | undefined {
  try {
    return readFileSync(abs);
  } catch (err) {
    if (isEnoent(err)) return undefined;
    log(`read failed ${abs}`, err);
    throw new SkillReadError(abs, err);
  }
}

// --- The repo snapshot → `files` map -----------------------------------------

/**
 * The two OpenAPI contract shapes admitted into a turn snapshot alongside the
 * agent-authored sources below: the produced contract
 * (`specs/design/components/<c>/openapi.yaml`) and a consumed contract
 * (`specs/design/components/<c>/dependencies/<dep>.openapi.yaml`). A
 * resolution/collab turn must be able to read back a spec it (or a prior turn)
 * just stored, so these two are admitted even though snapshots otherwise drop
 * `*.yaml` (e.g. `workload.yaml` stays excluded). `[^/]*` mirrors the Go side's
 * `path.Match` semantics — a `*` matches within one path segment only, never
 * crossing a `/`.
 */
const PRODUCED_SPEC_RE = /^specs\/design\/components\/[^/]*\/openapi\.yaml$/;
const CONSUMED_SPEC_RE = /^specs\/design\/components\/[^/]*\/dependencies\/[^/]*\.openapi\.yaml$/;

function isAdmittedSpecPath(path: string): boolean {
  return PRODUCED_SPEC_RE.test(path) || CONSUMED_SPEC_RE.test(path);
}

/**
 * The turn-snapshot filter — mirrors aep-api `agentfold.KeepInTurnSnapshot`:
 * keep agent-authored sources (`*.md`, `*.dsl`, `*.cell`, component
 * `design.json`, the acceptance oracle `validation-criteria.json`, the two
 * OpenAPI contract shapes above) and drop everything else (derived
 * `.excalidraw`/`*.gen.json` projections, code, arbitrary `*.yaml` such as
 * `workload.yaml`, …). `*.cell` is the project-level cell-diagram DSL
 * (design.cell) that drives the live architecture diagram.
 * validation-criteria.json is kept so a design regeneration can see the
 * existing oracle and preserve its covered flags instead of resetting them.
 */
export function keepInTurnSnapshot(path: string): boolean {
  if (path.endsWith(".md") || path.endsWith(".dsl") || path.endsWith(".cell")) return true;
  if (isAdmittedSpecPath(path)) return true;
  if (isTextReferencePath(path)) return true;
  const base = basename(path);
  return base === "design.json" || base === "validation-criteria.json";
}

/**
 * A user-uploaded reference the model should read AS TEXT.
 *
 * References are the one input here the agent did not author, so the
 * extension allow-list above — built for agent-authored spec artifacts — is the
 * wrong test for them: it admits a `.md` reference and silently drops a `.txt`
 * or `.csv` one, which then reaches the turn as a file on disk that nothing
 * puts in front of the model. The rule is the folder, not the extension.
 *
 * Natively-read binaries are excluded deliberately: they ride as file PARTS
 * (`readReferenceAttachments`), and admitting them here would either double
 * them up or, worse, pour a PDF's bytes into the text map — the failure this
 * channel exists to avoid.
 */
/**
 * Where reference documents appear inside a turn's snapshot (#383). NOT a repo
 * path: nothing is committed there any more — aep-api stores the documents off
 * git and overlays them into the extracted snapshot at this prefix (console
 * ADR-0017), which is exactly why nothing in this file had to change.
 */
export const REFERENCES_PREFIX = "specs/requirements/references/";

function isTextReferencePath(path: string): boolean {
  return path.startsWith(REFERENCES_PREFIX) && nativeMediaTypeFor(path) === undefined;
}

/**
 * Apply the walk's skip rules to an in-memory map (parity helper for callers
 * that mirror the server's view, e.g. the eval fold): dot-led path segments and
 * filtered-out paths drop; values are assumed text.
 */
export function filterTurnSnapshot(files: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [path, content] of Object.entries(files)) {
    if (path.split("/").some((seg) => seg.startsWith("."))) continue;
    if (!keepInTurnSnapshot(path)) continue;
    out[path] = content;
  }
  return out;
}

function walk(dir: string, rel: string, out: Record<string, string>): void {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (e.name.startsWith(".")) continue; // dot-entries (.git, .DS_Store, …)
    const abs = join(dir, e.name);
    const key = rel ? `${rel}/${e.name}` : e.name; // POSIX-relative key
    if (e.isDirectory()) {
      walk(abs, key, out);
      continue;
    }
    if (!e.isFile()) continue;
    if (!keepInTurnSnapshot(key)) continue;
    const buf = readFileSync(abs);
    if (buf.includes(0)) continue; // a NUL byte → binary; the agent only edits text
    out[key] = buf.toString("utf8");
  }
}

/**
 * Read one immutable per-SHA snapshot dir into the in-memory `files` map a turn
 * runs against (feeding `new FileBundle(map)` / `new TaskPlan(map)` and
 * `buildPrompt` byte-unchanged). Adapted from `playground/threads.ts`
 * `readSnapshot` (copied — `src/` must not import the playground tree).
 */
export function readSnapshot(snapshotDir: string): Record<string, string> {
  const out: Record<string, string> = {};
  walk(snapshotDir, "", out);
  return out;
}

// --- Reference PDFs → native AI SDK file parts (#384) -------------------------

/**
 * What one turn's attachments may add to the request, measured in ENCODED
 * bytes. Anthropic caps a Messages request at 32MB and an attachment rides it
 * base64 — 4 wire bytes per 3 raw — while the prompt, the text snapshot and the
 * whole conversation history share that same request. So attachments get a
 * budget, not the limit: 20MB encoded (~15MB of PDF) leaves ~12MB for
 * everything else.
 *
 * The budget is per TURN, not per file. A per-file cap alone cannot hold the
 * line twice over: a single 30MB PDF encodes to 40MB and blows the request on
 * its own, and three 8MB PDFs each pass any per-file check and still overrun it
 * together.
 */
export const MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES = 20 * 1024 * 1024;

/** Base64 length of `rawBytes` bytes: 4 chars per 3 bytes, rounded up with padding. */
function encodedLength(rawBytes: number): number {
  return Math.ceil(rawBytes / 3) * 4;
}

/**
 * The binary reference types the model reads natively, by extension: PDFs as
 * document blocks, images as image blocks. Anything else binary has no native
 * representation and is skipped (it is also invisible to the text snapshot,
 * which is exactly why these ride as parts at all).
 */
const NATIVE_MEDIA_BY_EXT: Record<string, string> = {
  pdf: "application/pdf",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
};

function nativeMediaTypeFor(path: string): string | undefined {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  return NATIVE_MEDIA_BY_EXT[ext];
}

interface ReferenceAttachment {
  part: FilePart;
  /** What this part costs the turn's budget — its base64 length, exactly. */
  encodedBytes: number;
}

/**
 * The snapshot dir under both spellings. `resolved` fences the requested path
 * before any I/O; `real` fences it again after symlinks are followed. They
 * differ whenever the mount itself sits behind a link (`/var` → `/private/var`
 * on macOS, and the shared workspace volume in the cluster), so a check against
 * the wrong one refuses every honest reference.
 */
interface SnapshotBounds {
  resolved: string;
  real: string;
}

/**
 * Read one reference's bytes as a `FilePart` of `mediaType`, or `undefined` on
 * any best-effort skip (never throws): outside the snapshot dir (a hostile or
 * malformed path, or a symlink pointing out of it — see below), missing,
 * unreadable, or more encoded bytes than `remainingEncodedBudget` still allows.
 * `data` is base64 — the AI SDK accepts a bare base64 string as `DataContent`,
 * and it keeps the eventual conversation-history JSON compact (a `Buffer` would
 * serialize as a giant `{type:"Buffer",data:[...]}` byte array instead).
 *
 * The bounds check runs twice, on purpose. `resolve` collapses `..` without
 * touching the filesystem, so a hostile path is refused before any I/O; then
 * `realpathSync` resolves every symlink on the way down, because the snapshot is
 * a checkout of a user's repo and a committed symlink — the file itself, or any
 * parent dir — would otherwise pass a textual prefix check and hand the model a
 * file from outside the snapshot.
 */
function readOneReferenceAttachment(
  snapshotDir: string,
  refPath: string,
  bounds: SnapshotBounds,
  mediaType: string,
  remainingEncodedBudget: number,
): ReferenceAttachment | undefined {
  const abs = resolve(snapshotDir, refPath);
  if (!isWithin(abs, bounds.resolved)) {
    console.warn(`[references] skipping reference outside the snapshot: ${refPath}`);
    return undefined;
  }
  let real: string;
  let size: number;
  try {
    real = realpathSync(abs);
    size = statSync(real).size;
  } catch (err) {
    console.warn(`[references] skipping unreadable reference ${refPath}: ${err instanceof Error ? err.message : String(err)}`);
    return undefined;
  }
  if (!isWithin(real, bounds.real)) {
    console.warn(`[references] skipping reference that links outside the snapshot: ${refPath}`);
    return undefined;
  }
  const encodedBytes = encodedLength(size);
  if (encodedBytes > remainingEncodedBudget) {
    console.warn(
      `[references] skipping oversized reference ${refPath}: ${size} bytes (${encodedBytes} encoded) > ${remainingEncodedBudget} bytes left of the turn's ${MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES} encoded budget`,
    );
    return undefined;
  }
  let bytes: Buffer;
  try {
    bytes = readFileSync(real);
  } catch (err) {
    console.warn(`[references] failed to read reference ${refPath}: ${err instanceof Error ? err.message : String(err)}`);
    return undefined;
  }
  const data = bytes.toString("base64");
  return {
    part: { type: "file", data, mediaType, filename: refPath },
    encodedBytes: data.length,
  };
}

/** Is `abs` the snapshot root itself, or something under it? */
function isWithin(abs: string, root: string): boolean {
  return abs === root || abs.startsWith(root + sep);
}

function snapshotBounds(snapshotDir: string): SnapshotBounds {
  const resolved = resolve(snapshotDir);
  try {
    return { resolved, real: realpathSync(resolved) };
  } catch {
    // An unreadable root is not this function's error to raise: every reference
    // under it then fails its own read and is warned and skipped.
    return { resolved, real: resolved };
  }
}

/**
 * Attach every natively-readable binary reference (case-insensitive `.pdf`,
 * `.png`, `.jpg`/`.jpeg`) in a `start` turn's `TurnSpec.references` as a
 * native AI SDK file part — Anthropic reads PDFs as document blocks and
 * images as image blocks, so the model sees the actual mockup or form rather
 * than pulling bytes through a tool as "text" (see the module doc for why
 * that mattered). Text references (`.md`/`.txt`) are already inlined by
 * `readSnapshot` and are left alone here. Absent/empty `references` → `[]`,
 * so a turn with no attachable references builds byte-identical messages to
 * before this existed.
 *
 * References are attached in the order given until the turn's encoded budget
 * runs out; each one that does not fit is warned and skipped, and the rest still
 * get their chance (a 40MB PDF listed first must not starve the 200KB brief
 * behind it).
 */
export function readReferenceAttachments(snapshotDir: string, references: string[] | undefined): FilePart[] {
  const bounds = snapshotBounds(snapshotDir);
  const parts: FilePart[] = [];
  let spent = 0;
  for (const raw of references ?? []) {
    const refPath = raw.trim();
    if (refPath === "") continue;
    const mediaType = nativeMediaTypeFor(refPath);
    if (mediaType === undefined) continue;
    const attachment = readOneReferenceAttachment(
      snapshotDir,
      refPath,
      bounds,
      mediaType,
      MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES - spent,
    );
    if (attachment === undefined) continue;
    parts.push(attachment.part);
    spent += attachment.encodedBytes;
  }
  return parts;
}

/**
 * Overlay the SNAPSHOT's reference-document texts onto a room-scoped turn's
 * files. The collab room deliberately excludes reference documents (they are
 * inputs, not collaboratively-edited spec), so a turn whose CURRENT STATE comes
 * from the room would silently lose the user's text references — a live /start
 * did exactly that: the steer listed claim.md, the snapshot held it, and the
 * prompt never saw it. The snapshot is the authority for references, so a stale
 * room copy (seeded before the exclusion existed, or from a project created
 * under the feature's v1) is overwritten, and the room stays the authority for
 * everything else.
 */
export function overlayReferenceTexts(
  roomFiles: Record<string, string>,
  snapshotFiles: Record<string, string>,
): Record<string, string> {
  const refs = Object.entries(snapshotFiles).filter(([path]) => path.startsWith(REFERENCES_PREFIX));
  if (refs.length === 0) return roomFiles;
  const out = { ...roomFiles };
  for (const [path, content] of refs) out[path] = content;
  return out;
}

/**
 * Chat attachments (#428) → native AI SDK file parts.
 *
 * The sibling of `readReferenceAttachments`, and the difference is where the
 * bytes come from: a reference is READ FROM THE SNAPSHOT by path, because the
 * platform stored it and overlaid it there. An attachment is never stored at all
 * (console ADR-0019), so its bytes arrive INLINE on the turn request and this
 * touches no filesystem — no path to fence, no symlink to resolve, no ENOENT to
 * survive.
 *
 * The same per-turn budget applies, and it is shared with the reference parts
 * rather than doubled: `spent` carries over, because the ceiling is a property of
 * the MODEL REQUEST, not of either channel. Attachments are taken in order until
 * the budget runs out; each one that does not fit is warned and skipped, and the
 * rest still get their chance.
 *
 * Best-effort per item, like every other reader here: a malformed base64 payload
 * is warned and skipped rather than failing the whole turn.
 */
export function toAttachmentParts(
  attachments: TurnAttachment[] | undefined,
  alreadySpentEncodedBytes = 0,
): FilePart[] {
  const parts: FilePart[] = [];
  let spent = alreadySpentEncodedBytes;
  for (const a of attachments ?? []) {
    const name = a.name.trim();
    if (name === "") continue;
    // `data` IS the encoded form, so its length is exactly what this costs the
    // budget — no need to decode to measure.
    const encodedBytes = a.data.length;
    if (spent + encodedBytes > MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES) {
      console.warn(
        `[attachments] skipping oversized attachment ${name}: ${encodedBytes} encoded bytes exceeds the ${MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES - spent} left of the turn's budget`,
      );
      continue;
    }
    parts.push({ type: "file", data: a.data, mediaType: a.mediaType, filename: name });
    spent += encodedBytes;
  }
  return parts;
}

// --- The `_skills` snapshot → lazy SkillSource --------------------------------

/** Split a `SKILL.md` into frontmatter fields + body (mirrors the caller-side skill resolver). */
function parseSkillMd(raw: string): {
  name?: string;
  description: string;
  body: string;
  audience: readonly SkillAudience[];
} {
  const text = lf(raw);
  const m = FRONTMATTER_RE.exec(text);
  const frontmatter = m?.[1] ?? "";
  const body = m ? text.slice(m[0].length) : text;
  let fm: Record<string, unknown> = {};
  if (frontmatter.trim() !== "") {
    try {
      const parsed = parseYaml(frontmatter) as unknown;
      if (parsed && typeof parsed === "object") fm = parsed as Record<string, unknown>;
    } catch {
      // Unparseable frontmatter → treat as absent; the dir name still names the skill.
    }
  }
  // `metadata.aep.audience` — which agents may load this skill (ADR-0013).
  // Unrecognised values are dropped rather than becoming a third audience; a
  // skill left with nothing declared resolves to EVERY audience, so an unmarked
  // (or misspelt) skill stays loadable instead of silently disappearing.
  const aep = (fm.metadata as Record<string, unknown> | undefined)?.aep as Record<string, unknown> | undefined;
  const declared = Array.isArray(aep?.audience) ? aep.audience : [];
  const audience = declared.filter((a): a is SkillAudience => a === "design" || a === "coding");
  return {
    ...(typeof fm.name === "string" && fm.name.trim() !== "" ? { name: fm.name } : {}),
    description: typeof fm.description === "string" ? fm.description : "",
    body,
    audience: audience.length > 0 ? audience : ALL_AUDIENCES,
  };
}

/**
 * Sorted relative paths of every aux file under one skill dir (readdir at call
 * time — D4-immutable): the Agent Skills standard structure carries SKILL.md
 * plus ANY supporting files — `references/*.md`, `scripts/*`, `assets/*`, or
 * arbitrary extras nested arbitrarily deep. Recurses the whole skill dir,
 * skipping the top-level SKILL.md and dot-entries (files and dirs, at any
 * depth) — the same skip rule the Go-side scan applies.
 */
function listReferences(skillDir: string, log: SkillReadLog = defaultSkillReadLog): string[] {
  const out: string[] = [];
  const walkAux = (dir: string, rel: string): void => {
    let entries: Dirent[];
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch (err) {
      if (isEnoent(err)) return;
      log(`readdir failed ${dir}`, err);
      throw new SkillReadError(dir, err);
    }
    for (const e of entries) {
      if (e.name.startsWith(".")) continue; // dot-entries (files and dirs) skipped
      if (rel === "" && e.name === "SKILL.md") continue; // the skill body itself, not an aux file
      const abs = join(dir, e.name);
      const key = rel ? `${rel}/${e.name}` : e.name;
      if (e.isDirectory()) {
        walkAux(abs, key);
        continue;
      }
      if (!e.isFile()) continue;
      out.push(key);
    }
  };
  walkAux(skillDir, "");
  return out.sort();
}

interface CatalogRow extends SkillCatalogEntry {
  /** Absolute skill dir — the lazy loaders read SKILL.md/references from here. */
  dir: string;
}

/**
 * The disk-backed `SkillSource` over one immutable `_skills` snapshot. The
 * constructor scans ONLY the catalog surface (frontmatter name/description +
 * whether references exist); bodies are read from disk when `loadSkill` /
 * `loadSkillReference` actually run.
 */
export class SnapshotSkillSource implements SkillSource {
  private readonly rows: CatalogRow[];
  private readonly byName: Map<string, CatalogRow>;
  private readonly log: SkillReadLog;

  constructor(skillsSnapshotDir: string, log: SkillReadLog = defaultSkillReadLog) {
    this.log = log;
    this.rows = scanCatalog(skillsSnapshotDir, log);
    this.byName = new Map(this.rows.map((r) => [r.name, r] as const));
  }

  catalog(): readonly SkillCatalogEntry[] {
    return this.rows.map(({ name, description, hasReferences, audience }) => ({
      name,
      description,
      hasReferences,
      audience,
    }));
  }

  load(name: string): SkillLoadResult {
    const row = this.byName.get(name);
    if (row === undefined) return undefined;
    // Audience gate before any disk read: this consumer may see the row (it
    // needs the name to pin the skill onto a component) but not the body.
    if (!row.audience.includes(SERVICE_AUDIENCE)) return { refused: true };
    const abs = join(row.dir, "SKILL.md");
    const raw = readSkillText(abs, this.log);
    if (raw === undefined) return undefined;
    return { content: parseSkillMd(raw).body.trim(), references: listReferences(row.dir, this.log) };
  }

  loadReference(name: string, path: string): LoadedReference {
    const row = this.byName.get(name);
    if (row === undefined) return undefined;
    // Fence-by-allowlist: the model-supplied path must be one of the LISTED
    // reference paths (never resolved raw against the fs).
    if (!listReferences(row.dir, this.log).includes(path)) return undefined;
    const abs = join(row.dir, path);
    const buf = readSkillBytes(abs, this.log);
    if (buf === undefined) return undefined;
    // Cheap UTF-8 validity check: re-encode the decoded text and compare bytes
    // — a mismatch means the file isn't valid UTF-8 text (binary), and
    // model-context surfaces must never inline binary.
    const text = buf.toString("utf8");
    if (!Buffer.from(text, "utf8").equals(buf)) return { binary: true };
    return { content: text };
  }
}

/**
 * The RETIRED kind path-segments of the pre-flat org-skills layout
 * (`skills/<kindDir>/<name>/`). Old per-SHA snapshots keep this shape forever,
 * so the scan tolerates it alongside the current flat layout.
 */
const LEGACY_KIND_DIRS = new Set(["builtin", "flow", "custom", "imported"]);

/**
 * Read the sidecar `skills-manifest.json` (ADR-0014) and return the set of
 * skill names an org admin has DISABLED. Availability FAILS OPEN: a missing
 * file, unreadable file, invalid JSON, a non-object root, or an entry that
 * isn't itself an object all yield an EMPTY set (nothing disabled) rather
 * than throwing — a malformed sidecar must never blank an org's whole
 * catalog, which would be far worse than serving a skill that should have
 * been hidden. Each entry is checked individually so one bad entry cannot
 * poison the read of the rest.
 */
function readDisabledSkillNames(snapshotDir: string): Set<string> {
  const disabled = new Set<string>();
  let raw: string;
  try {
    raw = readFileSync(join(snapshotDir, "skills-manifest.json"), "utf8");
  } catch {
    return disabled; // no manifest → nothing disabled
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return disabled; // unparseable → nothing disabled
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return disabled;
  for (const [name, entry] of Object.entries(parsed as Record<string, unknown>)) {
    if (entry !== null && typeof entry === "object" && (entry as Record<string, unknown>).disabled === true) {
      disabled.add(name);
    }
  }
  return disabled;
}

/**
 * Scan the snapshot's skill catalog into rows. The current shape is FLAT —
 * `<snapshotDir>/skills/<name>/SKILL.md` with the kind in frontmatter
 * (`metadata.aep.kind`; irrelevant to this scan) — and the legacy nested shape
 * `skills/<kindDir>/<name>/SKILL.md` is tolerated for old snapshots.
 * Deterministic order: flat dirs sorted first, then legacy kind dirs sorted
 * with skill dirs sorted within each; a duplicate skill NAME keeps its first
 * occurrence (so a flat copy wins over a legacy one). A dir whose SKILL.md is
 * missing (ENOENT) is simply not a skill; a SKILL.md that exists but cannot be
 * read (non-ENOENT I/O) fails the catalog load via `SkillReadError`. A snapshot
 * without `skills/` yields an empty catalog. A skill named in the
 * `skills-manifest.json` sidecar with `disabled: true` (ADR-0014) never becomes
 * a row at all — it is withheld from this org entirely, not merely
 * access-gated — so it never reaches `this.rows`/`this.byName` and
 * `load`/`loadReference` fall through to their "unknown name" branch for free.
 */
function scanCatalog(snapshotDir: string, log: SkillReadLog = defaultSkillReadLog): CatalogRow[] {
  const skillsRoot = join(snapshotDir, "skills");
  if (!existsSync(skillsRoot)) return [];
  const disabledNames = readDisabledSkillNames(snapshotDir);
  const rows: CatalogRow[] = [];
  const seen = new Set<string>();
  const addSkillDir = (dir: string, id: string): void => {
    const skillMd = join(dir, "SKILL.md");
    const raw = readSkillText(skillMd, log);
    if (raw === undefined) return; // ENOENT — not a skill
    const parsed = parseSkillMd(raw);
    const name = parsed.name ?? id; // fallback: the dir name IS the skill id
    if (seen.has(name)) return;
    seen.add(name);
    if (disabledNames.has(name)) return; // disabled → does not exist for this org
    rows.push({
      name,
      description: parsed.description,
      hasReferences: listReferences(dir, log).length > 0,
      audience: parsed.audience,
      dir,
    });
  };

  // Flat layout first (a dir holding SKILL.md directly IS a skill — even one
  // named like a kind word; those names are reserved server-side).
  const legacyKindDirs: string[] = [];
  for (const entry of listDirs(skillsRoot)) {
    const dir = join(skillsRoot, entry);
    if (existsSync(join(dir, "SKILL.md"))) {
      addSkillDir(dir, entry);
    } else if (LEGACY_KIND_DIRS.has(entry)) {
      legacyKindDirs.push(entry);
    }
  }
  for (const kind of legacyKindDirs) {
    for (const id of listDirs(join(skillsRoot, kind))) {
      addSkillDir(join(skillsRoot, kind, id), id);
    }
  }
  return rows;
}

function listDirs(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true })
    .filter((d) => d.isDirectory() && !d.name.startsWith("."))
    .map((d) => d.name)
    .sort((a, b) => a.localeCompare(b));
}

/** Build the lazy, disk-backed skill source over one `_skills` snapshot dir. */
export function loadSkillsFromSnapshot(skillsSnapshotDir: string): SnapshotSkillSource {
  return new SnapshotSkillSource(skillsSnapshotDir);
}
