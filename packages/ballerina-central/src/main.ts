/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * The process wrapper: argv in, exit code out.
 *
 * Kept apart from `cli.ts` so the CLI's behaviour is testable without a
 * subprocess and without a module that calls `process.exit` on import. This is
 * ALSO the only module that reads the environment or touches a filesystem the
 * caller did not name, which is what keeps every test in the suite hermetic: they
 * drive `run()` with an injected cache and cannot accidentally read or write a
 * developer's real `~/.cache`.
 */

import { homedir, tmpdir } from "node:os";
import { createDiskCache, isUsableRoot } from "./cache/disk.js";
import { resolveCacheCandidates } from "./cache/location.js";
import { NULL_CACHE, type DocsCache } from "./cache/store.js";
import { run } from "./cli.js";

/**
 * `homedir()` and `tmpdir()` read the environment and can throw on a container
 * with no passwd entry, which is a shape a runner can genuinely have. Anything
 * that goes wrong here means no cache, not no lookup.
 */
function safely(read: () => string): string {
  try {
    return read();
  } catch {
    return "";
  }
}

/**
 * The first candidate location that actually works.
 *
 * `resolveCacheCandidates` is pure and cannot tell whether a directory is
 * writable, so trying them is this module's job — it is already the only one
 * allowed to touch a filesystem the caller did not name. The case this exists for
 * is a container whose `$HOME` exists and is read-only, which is a shape a runner
 * genuinely has: without the retry, the default rung would be chosen, fail, and
 * silently disable caching rather than reaching `/tmp`.
 *
 * If every candidate fails, the null store is the answer. Never a failure: cache
 * trouble is not the caller's problem.
 */
function buildCache(): DocsCache {
  const uid = typeof process.getuid === "function" ? process.getuid() : 0;
  const candidates = resolveCacheCandidates({
    env: process.env,
    homedir: safely(homedir),
    tmpdir: safely(tmpdir),
    uid,
  });

  for (const candidate of candidates) {
    if (candidate.kind === "disabled") return NULL_CACHE;
    if (!isUsableRoot(candidate.root, candidate.mode, uid)) continue;
    return createDiskCache({ root: candidate.root, mode: candidate.mode, uid });
  }

  // Nothing worked. `describe()` still has to name something for `--help`, so the
  // last candidate is reported as the one that was tried.
  const last = candidates[candidates.length - 1];
  return last !== undefined && last.kind === "directory"
    ? createDiskCache({ root: last.root, mode: last.mode, uid })
    : NULL_CACHE;
}

const code = await run(
  process.argv.slice(2),
  {
    out: (text) => process.stdout.write(text),
    errorOut: (text) => process.stderr.write(text),
  },
  { cache: buildCache() },
).catch((cause: unknown) => {
  // Nothing in the pipeline throws by design; if something does, it is a defect
  // in this package and the caller still needs a machine-readable line.
  const message = cause instanceof Error ? cause.message : String(cause);
  process.stderr.write(`${JSON.stringify({ kind: "internal", message })}\n`);
  return 1;
});

process.exitCode = code;
