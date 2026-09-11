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

// Scrubber removes secrets from runner stdout before NDJSON emission.
// Two layers per docs/design/task-execution-progress.md §5.1:
//
//   1. Denylist — exact-string match against env-seeded secrets
//      (ANTHROPIC_API_KEY, the per-task bearer) plus regexes for
//      well-known token shapes and HTTP auth-header substrings.
//
//   2. Entropy backstop — long base64-ish substrings whose Shannon
//      entropy crosses a threshold are redacted. Catches secrets the
//      denylist doesn't know about.

const REDACTED = "[REDACTED]";

// Minimum literal length to enroll. Shorter strings are too noisy
// (would shred normal text — e.g. the literal "admin" or a 4-char id).
export const MIN_LITERAL_LEN = 12;

// Well-known token shapes. We redact only the value, preserving the
// header key (`Authorization:` / `x-api-key:`) so the line stays readable.
const TOKEN_PATTERNS: ReadonlyArray<RegExp> = [
  /\bghs_[A-Za-z0-9]{20,}\b/g,
  /\bghp_[A-Za-z0-9]{20,}\b/g,
  /\bgho_[A-Za-z0-9]{20,}\b/g,
  /\bghu_[A-Za-z0-9]{20,}\b/g,
  /\bghr_[A-Za-z0-9]{20,}\b/g,
  /\bgithub_pat_[A-Za-z0-9_]{20,}\b/g,
];

const HEADER_PATTERNS: ReadonlyArray<RegExp> = [
  // Bearer credential FIRST. The `authorization:` pattern below only consumes
  // the scheme word ("Bearer") — its `\S+` stops at the space before the token
  // — so running this first is what actually redacts the credential in an
  // `Authorization: Bearer <token>` string. Ordering matters now that the
  // entropy backstop (which used to catch the leaked token) is disabled.
  /(bearer\s+)([A-Za-z0-9._-]{16,})/gi,
  /(authorization\s*:\s*)(\S+)/gi,
  /(x-api-key\s*:\s*)(\S+)/gi,
  // `gh`'s own config shape. The credhelper branch writes the GitHub token at
  // rest as `oauth_token: <token>` into <workspace>/.gh-config/hosts.yml
  // (credhelper.ts, ghWrapperScript), inside the tree the agent works in — so
  // `cat`ting that file puts a live credential on the feed. That token is
  // minted inside bash and never enters this process, so no literal can be
  // enrolled for it: shape is the only layer available. Not a substitute for
  // enrollment, which is why the gap is recorded rather than closed here.
  /(oauth_token\s*:\s*)(\S+)/gi,
];

// Base64url-ish charset; minimum length 32 for the entropy backstop.
// Includes JWT-shaped strings (with dots). `=` is excluded from the
// candidate alphabet so the regex doesn't absorb a `key=value` boundary
// — base64 padding only ever appears at a string's tail anyway.
const ENTROPY_CANDIDATE = /[A-Za-z0-9_\-+/.]{32,}/g;
const ENTROPY_THRESHOLD = 4.0; // bits/char

// Entropy backstop toggle — DISABLED by default (2026-07-11). It over-redacts
// CamelCase file paths in tool_use summaries: the candidate alphabet above
// includes `/` and `.`, so a whole path such as
// src/components/Dashboard/BuildHistoryPanel.tsx is treated as one 32+ char
// token that scores >4.0 bits/char and is wholesale [REDACTED]. The denylist +
// token/header patterns still run, so known secrets (primed literals, GitHub
// tokens, auth/x-api-key/bearer headers) remain covered. Re-enable by setting
// RUNNER_SCRUB_ENTROPY=1 — but only after the candidate alphabet drops `/` so
// paths fragment into sub-32-char segments instead of matching as one token.
const ENTROPY_BACKSTOP_ENABLED = process.env.RUNNER_SCRUB_ENTROPY === "1";

function shannonEntropy(s: string): number {
  if (s.length === 0) return 0;
  const counts = new Map<string, number>();
  for (const c of s) counts.set(c, (counts.get(c) ?? 0) + 1);
  let h = 0;
  const n = s.length;
  for (const c of counts.values()) {
    const p = c / n;
    h -= p * Math.log2(p);
  }
  return h;
}

export class Scrubber {
  private literals: string[] = [];

  addLiteral(secret: string | undefined | null): void {
    if (!secret) return;
    if (secret.length < MIN_LITERAL_LEN) return;
    if (this.literals.includes(secret)) return;
    this.literals.push(secret);
    this.literals.sort((a, b) => b.length - a.length);
  }

  // Test seam.
  reset(): void {
    this.literals = [];
  }

  scrub(line: string): string {
    let out = line;

    for (const lit of this.literals) {
      if (out.includes(lit)) {
        out = out.split(lit).join(REDACTED);
      }
    }

    for (const re of TOKEN_PATTERNS) {
      out = out.replace(re, REDACTED);
    }

    for (const re of HEADER_PATTERNS) {
      out = out.replace(re, (_match, prefix: string) => `${prefix}${REDACTED}`);
    }

    if (ENTROPY_BACKSTOP_ENABLED) {
      out = out.replace(ENTROPY_CANDIDATE, (match) => {
        if (shannonEntropy(match) >= ENTROPY_THRESHOLD) return REDACTED;
        return match;
      });
    }

    return out;
  }
}

// Process-wide singleton — there is one stdout per pod, so one scrubber.
export const scrubber = new Scrubber();
