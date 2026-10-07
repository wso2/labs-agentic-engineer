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
 * The write gate for a web-application's prototype: its manifest
 * `specs/design/components/<component>/prototype.json` and its screens
 * `prototype.tsx` beside it, a React module over `@wso2/prototype-kit`.
 *
 * The rules are the kit's, the same ones `prototype check` and the Go save gate
 * apply: the manifest's JSON, version, shape and references (`/manifest`), then
 * the source's static rules and its literal references into the manifest
 * (`/source`). They run synchronously in `FileBundle`'s gate ladder
 * (`checkPrototype`).
 *
 * Drawing every screen executes the generated module, so this package does not:
 * the host passes a `PrototypeRenderCheck` (the agents service passes the kit's
 * isolated one) to `writeWithRenderCheck`, which runs it asynchronously, off
 * the host's event loop, before the write is made. Without one the files get
 * their static checks only (the console's mock replay).
 *
 * The pair is judged together. `prototype.tsx` is checked against the manifest
 * the bundle holds, so a screen is added or dropped in `prototype.json` and then
 * drawn in `prototype.tsx`; a `prototype.json` written while `prototype.tsx`
 * exists is checked against that source too (its references, then the render),
 * so a manifest change that breaks the screens is refused.
 *
 * Every refusal is `INVALID_PROTOTYPE` with the kit's findings, each keeping its
 * own code and place (`UNKNOWN_REFERENCE at flows[0].screenIds[1]`,
 * `FORBIDDEN_API at line 12`), because the place is what the model fixes.
 */

import { parseManifestJson } from "@wso2/prototype-kit/manifest";
import { checkSource, sourceReferenceFindings } from "@wso2/prototype-kit/source";
import type { FileBundle, PlannedWrite } from "./bundle.js";
import type { OpErr, OpResult, PrototypeFinding } from "./contracts/sse-events.js";

export interface PrototypeProblem {
  code: "INVALID_PROTOTYPE";
  message: string;
  findings: PrototypeFinding[];
}

/** The two files of a prototype that passed the static checks, as text. */
export interface PrototypeFileTexts {
  manifest: string;
  source: string;
}

/** Draws every screen for every role and display state; resolves to what failed to. */
export type PrototypeRenderCheck = (files: PrototypeFileTexts) => Promise<PrototypeFinding[]>;

/** Reads the bundle, for the other half of the pair. */
export interface PrototypeReader {
  read(path: string): string | undefined;
}

/** A write `writeWithRenderCheck` makes: the arguments of `addFile` or `editFile`. */
export type FileWrite = { op: "add"; path: string; content: string } | { op: "edit"; path: string; oldString: string; newString: string };

const PROTOTYPE_PATH = /^specs\/design\/components\/[^/]+\/prototype\.(json|tsx)$/;

/** Same move as every other gate: a refused write created nothing to edit. */
const REMEDY =
  "The write was refused and the file is unchanged. Fix every finding and retry: an editFile for a local " +
  "fix, or ONE addFile of the whole corrected file (removeFile first only if the file already existed).";

/** How many findings a refusal lists in its message before summarizing the rest. */
const MAX_LISTED = 8;

const sourcePathOf = (manifestPath: string) => manifestPath.replace(/prototype\.json$/, "prototype.tsx");
const manifestPathOf = (sourcePath: string) => sourcePath.replace(/prototype\.tsx$/, "prototype.json");

/**
 * Validate a candidate body for `path` on the kit's static rules. Returns null
 * when `path` is not a prototype file or the content is valid; otherwise the
 * problem, phrased for the model's self-correction.
 */
export function checkPrototype(path: string, content: string, bundle: PrototypeReader): PrototypeProblem | null {
  const kind = PROTOTYPE_PATH.exec(path)?.[1];
  if (kind === undefined) return null;
  if (kind === "json") {
    const manifest = parseManifestJson(content);
    if (!manifest.ok) return refuse(`${path} is not a valid prototype manifest`, manifest.findings);
    const sourcePath = sourcePathOf(path);
    const source = bundle.read(sourcePath);
    if (source === undefined) return null;
    const references = sourceReferenceFindings(source, manifest.manifest);
    return references.length > 0 ? refuse(brokenSource(path, sourcePath), references) : null;
  }

  const manifestPath = manifestPathOf(path);
  const manifestText = bundle.read(manifestPath);
  if (manifestText === undefined) {
    return refuse(`${path} is checked against its manifest`, [
      {
        code: "MISSING_FILE",
        file: "prototype.json",
        location: "(file)",
        message: `${manifestPath} does not exist yet; write it first (roles, states, screens, flows), then ${path}`,
      },
    ]);
  }
  const manifest = parseManifestJson(manifestText);
  if (!manifest.ok) {
    return refuse(`${path} is checked against ${manifestPath}, which is not valid; fix it first`, manifest.findings);
  }
  const staticFindings = checkSource(content);
  if (staticFindings.length > 0) return refuse(`${path} is not a valid prototype`, staticFindings);
  const references = sourceReferenceFindings(content, manifest.manifest);
  if (references.length > 0) return refuse(`${path} is not a valid prototype`, references);
  return null;
}

/**
 * Make a write through the bundle's gates and, when it leaves a whole
 * prototype pair, the host's render check, which runs asynchronously: a write
 * whose screens fail to draw is refused like any other gate's, with the
 * render findings, and the bundle is unchanged. Synchronous when there is
 * nothing to draw (any other file, a refusal, a no-op, half a pair). Writes to
 * one bundle must not interleave: the caller makes them one at a time, in
 * call order, waiting for a pending one.
 */
export function writeWithRenderCheck(bundle: FileBundle, write: FileWrite, render: PrototypeRenderCheck): OpResult | Promise<OpResult> {
  const plan = write.op === "add" ? bundle.planAdd(write.path, write.content) : bundle.planEdit(write.path, write.oldString, write.newString);
  if ("verdict" in plan) return plan.verdict;
  const make = () => (write.op === "add" ? bundle.addFile(write.path, write.content) : bundle.editFile(write.path, write.oldString, write.newString));
  const pair = renderPair(plan.write, bundle);
  if (!pair) return make();
  return render(pair.files).then((findings) => (findings.length > 0 ? renderRefusal(plan.write, pair.kind, findings) : make()));
}

/** The whole pair a planned prototype write leaves, or null when it leaves none to draw. */
function renderPair(write: PlannedWrite, bundle: PrototypeReader): { kind: "json" | "tsx"; files: PrototypeFileTexts } | null {
  const kind = PROTOTYPE_PATH.exec(write.path)?.[1] as "json" | "tsx" | undefined;
  if (kind === undefined) return null;
  const manifest = kind === "json" ? write.content : bundle.read(manifestPathOf(write.path));
  const source = kind === "tsx" ? write.content : bundle.read(sourcePathOf(write.path));
  return manifest === undefined || source === undefined ? null : { kind, files: { manifest, source } };
}

function renderRefusal(write: PlannedWrite, kind: "json" | "tsx", findings: PrototypeFinding[]): OpErr {
  const problem = refuse(kind === "json" ? brokenSource(write.path, sourcePathOf(write.path)) : `${write.path} is not a valid prototype`, findings);
  return { ok: false, path: write.path, op: write.op, code: problem.code, message: problem.message, findings: problem.findings };
}

function brokenSource(manifestPath: string, sourcePath: string): string {
  return `${manifestPath} would break ${sourcePath}, which is checked against it (to drop a screen, flow, role or state, first stop ${sourcePath} using it, then change the manifest)`;
}

function refuse(subject: string, findings: PrototypeFinding[]): PrototypeProblem {
  const listed = findings.slice(0, MAX_LISTED).map((f) => `${f.code} in ${f.file} at ${f.location}: ${f.message}`);
  const more = findings.length - listed.length;
  const detail = more > 0 ? `${listed.join("; ")}; and ${more} more` : listed.join("; ");
  return { code: "INVALID_PROTOTYPE", message: `${subject} - ${detail}. ${REMEDY}`, findings };
}
