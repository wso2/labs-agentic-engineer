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
import { CREDENTIAL_ENV_KEYS, scanCredentialEnv } from "./credential_env.js";
import { MIN_LITERAL_LEN, Scrubber } from "./progress/scrubber.js";

// A token whose SHAPE no TOKEN_PATTERN matches — same instrument as
// console_scrub.test.ts. Every test below turns on this distinction: a
// `ghp_`-prefixed value would be redacted by pattern whether or not anyone
// enrolled it, so it could not detect the bug this module fixes.
const OPAQUE_GIT_TOKEN = "aQ7fL2mZ9xR4tY6uP1sD3gH5jK8nB0vC";

function scrubberFor(env: NodeJS.ProcessEnv): Scrubber {
  const s = new Scrubber();
  for (const v of scanCredentialEnv(env).values) s.addLiteral(v);
  return s;
}

test("scanCredentialEnv: collects a mounted GITHUB_TOKEN", () => {
  const { values, tooShort } = scanCredentialEnv({ GITHUB_TOKEN: OPAQUE_GIT_TOKEN });
  assert.deepEqual(values, [OPAQUE_GIT_TOKEN]);
  assert.deepEqual(tooShort, []);
});

test("scanCredentialEnv: an empty GITHUB_TOKEN does not mask a set GH_TOKEN", () => {
  // The runner's own envHasGitHubToken() checks the two independently, so the
  // priming side has to as well.
  assert.deepEqual(
    scanCredentialEnv({ GITHUB_TOKEN: "", GH_TOKEN: OPAQUE_GIT_TOKEN }).values,
    [OPAQUE_GIT_TOKEN],
  );
});

test("scanCredentialEnv: unset and empty are neither enrolled nor reported", () => {
  // Nothing was mounted, so there is nothing to protect AND nothing to warn
  // about — an empty env must not produce a scary log line on every local run.
  assert.deepEqual(scanCredentialEnv({ GITHUB_TOKEN: "", ANTHROPIC_API_KEY: undefined }), {
    values: [],
    tooShort: [],
  });
  assert.deepEqual(scanCredentialEnv({}), { values: [], tooShort: [] });
});

test("scanCredentialEnv: boundary — MIN_LITERAL_LEN enrolls, one less is reported", () => {
  // The scrubber drops a literal shorter than MIN_LITERAL_LEN in silence, so
  // this boundary is the whole point of the partition. Both sides asserted
  // against the imported constant, not a hardcoded 12, so a threshold change
  // moves the test with it.
  const justLongEnough = "a".repeat(MIN_LITERAL_LEN);
  const justTooShort = "b".repeat(MIN_LITERAL_LEN - 1);

  assert.deepEqual(scanCredentialEnv({ GITHUB_TOKEN: justLongEnough }), {
    values: [justLongEnough],
    tooShort: [],
  });
  assert.deepEqual(scanCredentialEnv({ GITHUB_TOKEN: justTooShort }), {
    values: [],
    tooShort: ["GITHUB_TOKEN"],
  });
});

test("scanCredentialEnv: tooShort carries NAMES, never values", () => {
  // This list is built to be logged, so a value in it would be the disclosure
  // the module exists to prevent.
  const { values, tooShort } = scanCredentialEnv({
    GITHUB_TOKEN: "short",
    PUBLISHER_CLIENT_SECRET: "root",
  });
  assert.deepEqual(values, []);
  assert.deepEqual(tooShort, ["GITHUB_TOKEN", "PUBLISHER_CLIENT_SECRET"]);
  assert.ok(!tooShort.join(",").includes("short"));
  assert.ok(!tooShort.join(",").includes("root"));
});

test("a short credential is NOT redacted — which is why it is reported", () => {
  // Pins the consequence rather than the mechanism: the scrubber cannot cover
  // this value, so the run has to say so out loud instead of looking covered.
  const s = scrubberFor({ GITHUB_TOKEN: "short-tok" });
  assert.ok(s.scrub("token=short-tok").includes("short-tok"));
});

test("scanCredentialEnv: collects every mounted credential at once", () => {
  const { values } = scanCredentialEnv({
    GITHUB_TOKEN: "github-token-value-0001",
    GH_TOKEN: "gh-token-value-0002",
    ANTHROPIC_API_KEY: "anthropic-key-value-0003",
    CLAUDE_CODE_OAUTH_TOKEN: "claude-oauth-value-0004",
    PUBLISHER_CLIENT_SECRET: "publisher-secret-value-0005",
  });
  assert.equal(values.length, 5);
});

test("CREDENTIAL_ENV_KEYS: names the credentials the dispatch mounts", () => {
  // A MIRROR pin, and only that. The dispatch's source of truth is Go —
  // delivery/codingagent/oc_dispatcher.go (envAnthropicAPIKey, envGitHubToken)
  // and publisher.go (envPublisherClientSecret) — so a credential added THERE
  // breaks nothing here. What this test buys is that the two cannot drift
  // silently on this side: an edit to the list is a deliberate act with a
  // failing test in front of it. Keeping them in step is a review obligation,
  // the same way progress/schema.ts names its three mirrors.
  assert.deepEqual([...CREDENTIAL_ENV_KEYS], [
    "GITHUB_TOKEN",
    "GH_TOKEN",
    "ANTHROPIC_API_KEY",
    "CLAUDE_CODE_OAUTH_TOKEN",
    "PUBLISHER_CLIENT_SECRET",
  ]);
  // PUBLISHER_CLIENT_ID is an identifier, not a secret — enrolling it would
  // redact a value that appears in ordinary diagnostics.
  assert.ok(!CREDENTIAL_ENV_KEYS.includes("PUBLISHER_CLIENT_ID" as never));
});

test("priming from env redacts a git token whose shape no pattern matches", () => {
  const s = scrubberFor({ GITHUB_TOKEN: OPAQUE_GIT_TOKEN });
  const out = s.scrub(`fatal: unable to access 'https://x-access-token:${OPAQUE_GIT_TOKEN}@github.com/o/r'`);
  assert.ok(!out.includes(OPAQUE_GIT_TOKEN), "the git token survived into the log line");
  assert.match(out, /\[REDACTED\]/);
});

test("the same line leaks without enrollment — why priming is load-bearing", () => {
  // The regression this pins: shape patterns alone do NOT cover this token, so
  // an unprimed GITHUB_TOKEN reaches the user-visible build log verbatim.
  const out = new Scrubber().scrub(`token=${OPAQUE_GIT_TOKEN}`);
  assert.ok(out.includes(OPAQUE_GIT_TOKEN));
});

test("priming from env redacts the publisher client secret", () => {
  const s = scrubberFor({ PUBLISHER_CLIENT_SECRET: OPAQUE_GIT_TOKEN });
  assert.ok(!s.scrub(`-u id:${OPAQUE_GIT_TOKEN}`).includes(OPAQUE_GIT_TOKEN));
});
