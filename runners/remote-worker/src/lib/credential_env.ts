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

import { MIN_LITERAL_LEN } from "./progress/scrubber.js";

// The env var names the platform mounts a CREDENTIAL under — and the one place
// that list lives.
//
// Every entrypoint primes these into the progress scrubber before anything can
// log, because the BFF forwards this pod's console output verbatim into the
// user-visible build log. Enrollment is what actually redacts a value: the
// scrubber's shape patterns recognise only well-known GitHub token prefixes
// (`ghp_`, `github_pat_`, …), so a credential matching none of them reaches the
// log intact unless its literal was primed. GITHUB_TOKEN was exactly that case
// — enrolled nowhere, and covered by a prefix pattern or not at all.
//
// Deliberately a NAMED list, and not the deny-by-default sweep websearch_dlp.ts
// runs over this same environment. That sweep decides whether to BLOCK a web
// call, where a false positive costs one denied search; enrolling a literal here
// rewrites every line of output containing it, so a false positive shreds
// ordinary log text — the same over-redaction that got the scrubber's entropy
// backstop disabled. So: the credentials the PLATFORM mounts and can therefore
// name belong here, and a per-dependency secret (whose name is derived from the
// user's own dependency, hence unnameable here) stays covered by the DLP sweep.
//
// BOTH members of each pair are listed, because exactly one is typically set: a
// run bills either ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN (ADR-0016), and
// git resolves either GITHUB_TOKEN or GH_TOKEN. Priming only whichever happens
// to be set would leave the other unredacted. PUBLISHER_CLIENT_ID is absent on
// purpose — it is an identifier, not a secret, and websearch_dlp.ts treats it as
// safe for the same reason.
// KNOWN LIMIT — this list covers the credentials the dispatch MOUNTS, which is
// every credential on the env-token git path. It cannot cover the credhelper
// path (`GITHUB_TOKEN` unset): that token is minted inside bash by
// credhelper.sh and never enters this process, so there is no value to enroll.
// Its at-rest copy in `.gh-config/hosts.yml` is caught by shape only (the
// `oauth_token:` pattern in progress/scrubber.ts). Closing it properly means
// giving the helper a way to hand the runner what it minted.
export const CREDENTIAL_ENV_KEYS = [
  "GITHUB_TOKEN",
  "GH_TOKEN",
  "ANTHROPIC_API_KEY",
  "CLAUDE_CODE_OAUTH_TOKEN",
  "PUBLISHER_CLIENT_SECRET",
] as const;

/** What a scan of the environment found. Values on one side, NAMES on the other. */
export interface CredentialEnvScan {
  /** Values long enough for the scrubber to actually enroll. */
  values: string[];
  /**
   * The NAMES of mounted credentials whose values are too short to enroll —
   * never the values themselves, since this list is built to be logged.
   */
  tooShort: string[];
}

/**
 * Partition the mounted credentials into what the scrubber can enroll and what
 * it cannot.
 *
 * The split exists because `Scrubber.addLiteral` silently drops anything shorter
 * than MIN_LITERAL_LEN, and that threshold is not negotiable: a 4-character
 * literal would redact every occurrence of those characters in ordinary log
 * text, which is the over-redaction that disabled the entropy backstop. So a
 * short credential cannot be protected — and returning it anyway would produce
 * exactly the silent no-op that reads like coverage which this module was
 * written to end. It is reported by NAME instead, and the caller says so out
 * loud.
 *
 * Unset and empty entries are neither: nothing was mounted, so there is nothing
 * to protect and nothing to report.
 */
export function scanCredentialEnv(env: NodeJS.ProcessEnv = process.env): CredentialEnvScan {
  const values: string[] = [];
  const tooShort: string[] = [];
  for (const key of CREDENTIAL_ENV_KEYS) {
    const value = env[key];
    if (value === undefined || value === "") continue;
    if (value.length < MIN_LITERAL_LEN) {
      tooShort.push(key);
      continue;
    }
    values.push(value);
  }
  return { values, tooShort };
}
