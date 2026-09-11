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

import { test } from "node:test";
import assert from "node:assert/strict";
import { Scrubber } from "./scrubber.js";

function fresh(): Scrubber {
  return new Scrubber();
}

test("scrub: passes through unrelated text unchanged", () => {
  const s = fresh();
  s.addLiteral("super-secret-bearer-1234567890");
  const out = s.scrub("services/auth/jwt.go updated");
  assert.equal(out, "services/auth/jwt.go updated");
});

test("scrub: redacts a primed literal", () => {
  const s = fresh();
  s.addLiteral("super-secret-bearer-1234567890");
  const out = s.scrub("Authorization: super-secret-bearer-1234567890 value");
  assert.match(out, /\[REDACTED\]/);
  assert.ok(!out.includes("super-secret-bearer-1234567890"));
});

test("scrub: redacts a primed literal multiple times in one line", () => {
  const s = fresh();
  s.addLiteral("super-secret-bearer-1234567890");
  const out = s.scrub("super-secret-bearer-1234567890 then super-secret-bearer-1234567890 again");
  assert.equal(
    out,
    "[REDACTED] then [REDACTED] again",
  );
});

test("scrub: ignores literals shorter than min length", () => {
  const s = fresh();
  s.addLiteral("short"); // <12 chars — ignored
  const out = s.scrub("the word short appears here");
  assert.equal(out, "the word short appears here");
});

test("scrub: ignores empty / null / undefined literals", () => {
  const s = fresh();
  s.addLiteral("");
  s.addLiteral(null);
  s.addLiteral(undefined);
  const out = s.scrub("nothing should change");
  assert.equal(out, "nothing should change");
});

test("scrub: longer literal wins over substring overlap", () => {
  const s = fresh();
  s.addLiteral("AAAAAAAAAAAA-SHORT");
  s.addLiteral("AAAAAAAAAAAA-SHORT-AND-LONGER");
  const out = s.scrub("token=AAAAAAAAAAAA-SHORT-AND-LONGER end");
  assert.equal(out, "token=[REDACTED] end");
});

test("scrub: redacts ghs_ token", () => {
  const s = fresh();
  const line = "got token ghs_aBCDEFGhijklmnop1234567890 from gh wrapper";
  const out = s.scrub(line);
  assert.match(out, /got token \[REDACTED\] from gh wrapper/);
});

test("scrub: redacts ghp_ and github_pat_ tokens", () => {
  const s = fresh();
  const line = "user PAT ghp_abcdefghijklmnopqrstuv1234 also github_pat_11AAAA_abcdefghijklmnopqrst";
  const out = s.scrub(line);
  assert.ok(!out.includes("ghp_abcdefghijklmnopqrstuv1234"));
  assert.ok(!out.includes("github_pat_11AAAA_abcdefghijklmnopqrst"));
});

test("scrub: preserves Authorization header key, redacts value", () => {
  const s = fresh();
  const line = "curl ... -H \"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature1234567890\" ok";
  const out = s.scrub(line);
  assert.match(out, /Authorization\s*:\s*\[REDACTED\]/i);
  assert.ok(!out.includes("eyJhbGciOiJIUzI1NiJ9.payload.signature1234567890"));
});

test("scrub: redacts x-api-key value", () => {
  const s = fresh();
  const line = "headers: x-api-key: sk-ant-1234567890abcdefghijklmnop ;";
  const out = s.scrub(line);
  assert.match(out, /x-api-key\s*:\s*\[REDACTED\]/i);
});

test("scrub: entropy backstop disabled — high-entropy base64 substring passes through", () => {
  const s = fresh();
  // With RUNNER_SCRUB_ENTROPY unset the entropy layer is off (default), so an
  // unknown high-entropy blob is NOT caught — only the denylist + token/header
  // patterns run. See ENTROPY_BACKSTOP_ENABLED in scrubber.ts. If this flips
  // red, someone re-enabled the backstop; make that a conscious choice.
  const line = "credential=A7Bk39fZqLpNc2RxYwUv0sGmH4dT8jWoEi end";
  const out = s.scrub(line);
  assert.equal(out, line);
});

test("scrub: CamelCase file-path summary survives (entropy backstop disabled)", () => {
  const s = fresh();
  // Regression for the reason the backstop is disabled: this path scores >4.0
  // bits/char and used to be wholesale [REDACTED], blanking Write tool_use
  // summaries. With the backstop off the path stays readable.
  const line = "Write src/components/Dashboard/BuildHistoryPanel.tsx";
  const out = s.scrub(line);
  assert.equal(out, line);
});

test("scrub: leaves low-entropy long substring alone (false-positive guard)", () => {
  const s = fresh();
  // 40 'a' chars — long enough to match length but entropy is 0.
  const line = "filler=" + "a".repeat(40) + " end";
  const out = s.scrub(line);
  assert.equal(out, line);
});

test("scrub: leaves moderate-entropy short paths alone", () => {
  const s = fresh();
  const line = "edited services/auth/jwt.go and services/auth/middleware.go";
  const out = s.scrub(line);
  assert.equal(out, line);
});

test("scrub: redacts a JWT-shaped string via entropy backstop", () => {
  const s = fresh();
  const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c";
  const line = `bearer ${jwt} accepted`;
  const out = s.scrub(line);
  // Either the bearer-pattern OR the entropy backstop should catch it.
  assert.ok(!out.includes(jwt));
});

test("scrub: order — literal beats entropy (no double-redact garble)", () => {
  const s = fresh();
  const secret = "X".repeat(40); // would also trigger entropy if not low-entropy
  s.addLiteral(secret);
  const line = `prefix ${secret} suffix`;
  const out = s.scrub(line);
  assert.equal(out, "prefix [REDACTED] suffix");
});

test("scrub: redacts a token inside a failed `git clone` command string", () => {
  // The leak that motivated moving the clone token out of the URL: node's
  // child_process prefixes the failing COMMAND to the error message, and the
  // runner logs that. The clone no longer embeds credentials, but a shaped
  // token appearing here must still be caught.
  const s = fresh();
  const pat = "github_pat_11TESTONLY_abcdefghijklmnopqrstuvwxyz0123456789";
  const line =
    `Command failed: git clone 'https://x-access-token:${pat}@github.com/asdlc-repos/store.git' '/home/aep/ws'`;
  const out = s.scrub(line);
  assert.ok(!out.includes(pat), `leaked: ${out}`);
  assert.match(out, /git clone 'https:\/\/x-access-token:\[REDACTED\]@github\.com/);
});

test("scrub: redacts an enrolled token whose shape no pattern matches", () => {
  // Shape-independence: an enrolled literal is redacted whatever it looks like,
  // so a classic 40-hex PAT or an opaque App token is covered without the
  // denylist knowing its prefix.
  const s = fresh();
  const opaque = "aQ7fL2mZ9xR4tY6uP1sD3gH5jK8nB0vC";
  s.addLiteral(opaque);
  const line = `Command failed: git clone 'https://x-access-token:${opaque}@github.com/o/r.git' '/ws'`;
  const out = s.scrub(line);
  assert.ok(!out.includes(opaque), `leaked: ${out}`);
});

test("scrub: handles multi-line strings", () => {
  const s = fresh();
  s.addLiteral("super-secret-bearer-1234567890");
  const out = s.scrub("line1\nsuper-secret-bearer-1234567890\nline3");
  assert.equal(out, "line1\n[REDACTED]\nline3");
});

// A run authenticates with EXACTLY ONE of ANTHROPIC_API_KEY /
// CLAUDE_CODE_OAUTH_TOKEN — an org may bill its coding agent to a Claude
// subscription instead of API credits. Both entrypoints therefore prime the
// scrubber with both names: priming only one would leave the credential
// unredacted in the progress feed on precisely the runs that use the other.
test("scrub: an unset credential name does not disturb the one that is set", () => {
  const s = fresh();
  for (const v of [undefined, "sk-ant-oat01-subscription-token"]) s.addLiteral(v);
  const out = s.scrub("auth failed for sk-ant-oat01-subscription-token");
  assert.match(out, /\[REDACTED\]/);
  assert.ok(!out.includes("sk-ant-oat01-subscription-token"));
});

test("scrub: redacts an OAuth token exactly as it does an API key", () => {
  for (const secret of ["sk-ant-api03-a-console-api-key", "sk-ant-oat01-a-subscription-token"]) {
    const s = fresh();
    s.addLiteral(secret);
    const out = s.scrub(`ANTHROPIC error: ${secret} rejected`);
    assert.ok(!out.includes(secret), `${secret} leaked`);
  }
});

test("scrub: redacts a gh hosts.yml oauth_token, preserving the key", () => {
  // The credhelper branch writes the GitHub token at rest in the agent's own
  // workspace as `oauth_token: <token>`; `cat`ting it must not put a live
  // credential on the feed. Shape-only by necessity — that token is minted
  // inside bash and never enters the runner process, so nothing can enroll it.
  const s = fresh();
  const out = s.scrub("    oauth_token: aQ7fL2mZ9xR4tY6uP1sD3gH5jK8nB0vC");
  assert.match(out, /oauth_token: \[REDACTED\]/);
  assert.ok(!out.includes("aQ7fL2mZ9xR4tY6uP1sD3gH5jK8nB0vC"));
});

test("scrub: leaves an oauth_token key with no value alone", () => {
  const s = fresh();
  assert.equal(s.scrub("oauth_token:"), "oauth_token:");
});
