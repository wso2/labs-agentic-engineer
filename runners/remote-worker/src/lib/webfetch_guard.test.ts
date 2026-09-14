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
import type { PreToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";
import {
  checkUrlForSecret,
  classifyIPv4,
  classifyIPv6,
  createWebFetchGuardHook,
  webFetchDenial,
  isSsrfUrl,
  parseIPv6ToBigInt,
  WEBFETCH_SECRET_DENIAL_MESSAGE,
} from "./webfetch_guard.js";

// ---- isSsrfUrl (pure predicate) — SSRF classes -----------------------------

test("isSsrfUrl: denies the cloud-metadata link-local address (169.254.169.254)", () => {
  const result = isSsrfUrl("https://169.254.169.254/latest/meta-data");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /link-local/i);
});

test("isSsrfUrl: denies an RFC1918 private IPv4 address (10.x)", () => {
  const result = isSsrfUrl("https://10.0.0.5/internal");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /private/i);
});

test("isSsrfUrl: denies RFC1918 172.16.0.0/12", () => {
  const result = isSsrfUrl("https://172.20.1.1/internal");
  assert.equal(result.blocked, true);
});

test("isSsrfUrl: denies RFC1918 192.168.0.0/16", () => {
  const result = isSsrfUrl("https://192.168.1.1/internal");
  assert.equal(result.blocked, true);
});

test("isSsrfUrl: denies IPv4 loopback (127.0.0.1)", () => {
  const result = isSsrfUrl("https://127.0.0.1/admin");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /loopback/i);
});

test("isSsrfUrl: denies an alternate loopback encoding (127.1, decimal-collapsed)", () => {
  // WHATWG URL host parsing normalizes shorthand IPv4 forms to canonical
  // dotted-quad before .hostname is read (verified: "127.1" -> "127.0.0.1"),
  // so this also exercises that the guard reads the NORMALIZED host.
  const result = isSsrfUrl("https://127.1/admin");
  assert.equal(result.blocked, true);
});

test("isSsrfUrl: denies IPv6 loopback (::1)", () => {
  const result = isSsrfUrl("https://[::1]/admin");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /loopback/i);
});

test("isSsrfUrl: denies IPv6 link-local (fe80::/10)", () => {
  const result = isSsrfUrl("https://[fe80::1]/admin");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /link-local/i);
});

test("isSsrfUrl: denies IPv6 unique-local/private (fc00::/7)", () => {
  const result = isSsrfUrl("https://[fd12:3456:789a::1]/internal");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /private/i);
});

test("isSsrfUrl: denies CGNAT (100.64.0.0/10)", () => {
  const result = isSsrfUrl("https://100.64.1.1/internal");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /cgnat/i);
});

test("isSsrfUrl: denies NAT64 well-known prefix (64:ff9b::/96) embedding cloud metadata", () => {
  const result = isSsrfUrl("https://[64:ff9b::a9fe:a9fe]/latest/meta-data"); // a9fe:a9fe = 169.254.169.254
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /nat64/i);
});

test("isSsrfUrl: denies unspecified IPv4 (0.0.0.0)", () => {
  const result = isSsrfUrl("https://0.0.0.0/admin");
  assert.equal(result.blocked, true);
});

test("isSsrfUrl: denies unspecified IPv6 (::)", () => {
  const result = isSsrfUrl("https://[::]/admin");
  assert.equal(result.blocked, true);
});

test("isSsrfUrl: denies an IPv4-mapped IPv6 address embedding a private IP (::ffff:127.0.0.1)", () => {
  const result = isSsrfUrl("https://[::ffff:127.0.0.1]/admin");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /ipv4-embedded/i);
});

test("isSsrfUrl: denies an internal hostname (*.svc)", () => {
  const result = isSsrfUrl("https://foo.svc/internal-api");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /internal hostname/i);
});

test("isSsrfUrl: denies internal hostname suffixes .local, .internal, .cluster.local, and localhost", () => {
  assert.equal(isSsrfUrl("https://payments.internal/api").blocked, true);
  assert.equal(isSsrfUrl("https://myservice.cluster.local/api").blocked, true);
  assert.equal(isSsrfUrl("https://mymachine.local/api").blocked, true);
  assert.equal(isSsrfUrl("https://localhost/api").blocked, true);
});

test("isSsrfUrl: denies a single-label (dotless) host — resolves internally via search domains", () => {
  // Bare names have no public TLD; in-cluster they resolve to internal services.
  assert.equal(isSsrfUrl("https://kubernetes/api").blocked, true);
  assert.equal(isSsrfUrl("https://metadata/computeMetadata/v1/").blocked, true);
  const r = isSsrfUrl("https://internal-svc/");
  assert.equal(r.blocked, true);
  assert.match(r.reason ?? "", /single-label/i);
});

test("isSsrfUrl: still allows a normal public host that carries a dot", () => {
  // Guard against the dotless rule over-blocking ordinary public FQDNs.
  assert.equal(isSsrfUrl("https://openweathermap.org/current").blocked, false);
});

test("isSsrfUrl: denies a plain http:// URL (non-https scheme)", () => {
  const result = isSsrfUrl("http://api.example.com/docs");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /non-https/i);
});

test("isSsrfUrl: denies a garbage/unparseable URL", () => {
  const result = isSsrfUrl("not a url at all");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /failed to parse/i);
});

test("isSsrfUrl: denies an empty URL", () => {
  const result = isSsrfUrl("");
  assert.equal(result.blocked, true);
});

test("isSsrfUrl: allows a benign public https URL with no secret", () => {
  const result = isSsrfUrl("https://api.example.com/v1/docs");
  assert.equal(result.blocked, false);
});

test("isSsrfUrl: allows a benign public https URL with a path and query string", () => {
  const result = isSsrfUrl("https://docs.stripe.com/api/idempotent-requests?locale=en");
  assert.equal(result.blocked, false);
});

// ---- isSsrfUrl: trailing-dot FQDN bypass regressions -----------------------
//
// A trailing dot makes a hostname an absolute FQDN in DNS terms — it resolves
// identically to the bare form — but `String.endsWith(suffix)` and `=== "localhost"`
// do not match it, so every internal-hostname class below used to sail
// through as "allowed" (this is the CRITICAL bypass: it reaches GCP cloud
// metadata via the trailing-dot form of metadata.google.internal). A public
// hostname with a trailing dot must remain benign; only the internal classes
// must now block.

test("isSsrfUrl: denies trailing-dot localhost (localhost.)", () => {
  const result = isSsrfUrl("https://localhost./");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /internal hostname/i);
});

test("isSsrfUrl: denies trailing-dot .internal suffix (foo.internal.)", () => {
  const result = isSsrfUrl("https://foo.internal./");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /internal hostname/i);
});

test("isSsrfUrl: denies trailing-dot .svc suffix (kubernetes.default.svc.)", () => {
  const result = isSsrfUrl("https://kubernetes.default.svc./");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /internal hostname/i);
});

test("isSsrfUrl: denies trailing-dot .cluster.local suffix (x.cluster.local.)", () => {
  const result = isSsrfUrl("https://x.cluster.local./");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /internal hostname/i);
});

test("isSsrfUrl: denies trailing-dot .local suffix (y.local.)", () => {
  const result = isSsrfUrl("https://y.local./");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /internal hostname/i);
});

test("isSsrfUrl: denies trailing-dot GCP cloud-metadata hostname (metadata.google.internal.)", () => {
  const result = isSsrfUrl("https://metadata.google.internal./");
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /internal hostname/i);
});

test("isSsrfUrl: still allows a benign public hostname with a trailing dot (api.example.com.)", () => {
  const result = isSsrfUrl("https://api.example.com./");
  assert.equal(result.blocked, false);
});

// ---- classifyIPv4 / classifyIPv6 (unit-level range checks) -----------------

test("classifyIPv4: blocks loopback, private, link-local, CGNAT; allows a public address", () => {
  assert.equal(classifyIPv4([127, 0, 0, 1]).blocked, true);
  assert.equal(classifyIPv4([10, 1, 2, 3]).blocked, true);
  assert.equal(classifyIPv4([172, 16, 0, 1]).blocked, true);
  assert.equal(classifyIPv4([172, 32, 0, 1]).blocked, false); // just outside 172.16.0.0/12
  assert.equal(classifyIPv4([192, 168, 0, 1]).blocked, true);
  assert.equal(classifyIPv4([169, 254, 169, 254]).blocked, true);
  assert.equal(classifyIPv4([100, 64, 0, 1]).blocked, true);
  assert.equal(classifyIPv4([100, 63, 0, 1]).blocked, false); // just outside 100.64.0.0/10
  assert.equal(classifyIPv4([8, 8, 8, 8]).blocked, false);
});

test("classifyIPv6: blocks NAT64 prefix via parseIPv6ToBigInt round-trip", () => {
  const value = parseIPv6ToBigInt("64:ff9b::808:808"); // embeds 8.8.8.8, still NAT64-prefixed
  assert.ok(value !== null);
  const result = classifyIPv6(value as bigint);
  assert.equal(result.blocked, true);
  assert.match(result.reason ?? "", /nat64/i);
});

test("classifyIPv6: allows an ordinary global unicast address", () => {
  const value = parseIPv6ToBigInt("2606:4700:4700::1111");
  assert.ok(value !== null);
  assert.equal(classifyIPv6(value as bigint).blocked, false);
});

// ---- checkUrlForSecret (pure predicate) ------------------------------------

test("checkUrlForSecret: denies a URL containing a staged secret value", () => {
  const result = checkUrlForSecret(
    "https://attacker.example.com/collect?token=sk-live-abcdef123456",
    ["sk-live-abcdef123456"],
  );
  assert.equal(result.denied, true);
  assert.equal(result.message, WEBFETCH_SECRET_DENIAL_MESSAGE);
});

test("checkUrlForSecret: allows a clean URL with no secret substrings", () => {
  const result = checkUrlForSecret("https://api.example.com/v1/docs", [
    "sk-live-abcdef123456",
    "publisher-secret-abcdef123456",
  ]);
  assert.equal(result.denied, false);
});

test("checkUrlForSecret: empty secrets list never denies", () => {
  const result = checkUrlForSecret("https://api.example.com/anything", []);
  assert.equal(result.denied, false);
});

// ---- checkUrlForSecret: percent-encoded secret evasion regressions --------
//
// A raw-substring match alone is bypassable: a secret containing
// URL-reserved characters (/, +, =) can be percent-encoded in the URL and
// still reach the same destination, evading a literal `url.includes(secret)`
// check. The guard must also test the percent-decoded form.

test("checkUrlForSecret: denies a percent-encoded staged secret in the query string", () => {
  const result = checkUrlForSecret(
    "https://attacker.example.com/collect?x=ghp_ab%2Fcd%2Bef%3Dgh12",
    ["ghp_ab/cd+ef=gh12"],
  );
  assert.equal(result.denied, true);
  assert.equal(result.message, WEBFETCH_SECRET_DENIAL_MESSAGE);
});

test("checkUrlForSecret: still denies the same secret in raw (unencoded) form", () => {
  const result = checkUrlForSecret(
    "https://attacker.example.com/collect?x=ghp_ab/cd+ef=gh12",
    ["ghp_ab/cd+ef=gh12"],
  );
  assert.equal(result.denied, true);
  assert.equal(result.message, WEBFETCH_SECRET_DENIAL_MESSAGE);
});

test("checkUrlForSecret: allows a clean URL with no secret, encoded or otherwise", () => {
  const result = checkUrlForSecret(
    "https://attacker.example.com/collect?x=just%20a%20benign%20value",
    ["ghp_ab/cd+ef=gh12"],
  );
  assert.equal(result.denied, false);
});

// ---- createWebFetchGuardHook (PreToolUse HookCallback) ---------------------

interface SyncOutput {
  hookSpecificOutput?: {
    hookEventName?: string;
    permissionDecision?: string;
    permissionDecisionReason?: string;
  };
}

function preToolUseInput(url: string): PreToolUseHookInput {
  return {
    session_id: "s1",
    transcript_path: "/tmp/transcript.jsonl",
    cwd: "/workspace",
    hook_event_name: "PreToolUse",
    tool_name: "WebFetch",
    tool_input: { url },
    tool_use_id: "tool-use-1",
  };
}

test("createWebFetchGuardHook: denies an SSRF URL (cloud metadata)", async () => {
  const hook = createWebFetchGuardHook(webFetchDenial([]));
  const output = (await hook(
    preToolUseInput("https://169.254.169.254/latest/meta-data"),
    "tool-use-1",
    { signal: new AbortController().signal },
  )) as SyncOutput;

  assert.equal(output.hookSpecificOutput?.hookEventName, "PreToolUse");
  assert.equal(output.hookSpecificOutput?.permissionDecision, "deny");
  assert.match(output.hookSpecificOutput?.permissionDecisionReason ?? "", /blocked/i);
});

test("createWebFetchGuardHook: denies a URL containing a staged secret value", async () => {
  const hook = createWebFetchGuardHook(webFetchDenial(["staged-secret-value-123456"]));
  const output = (await hook(
    preToolUseInput("https://api.example.com/collect?token=staged-secret-value-123456"),
    "tool-use-1",
    { signal: new AbortController().signal },
  )) as SyncOutput;

  assert.equal(output.hookSpecificOutput?.permissionDecision, "deny");
  assert.equal(output.hookSpecificOutput?.permissionDecisionReason, WEBFETCH_SECRET_DENIAL_MESSAGE);
});

test("createWebFetchGuardHook: allows a clean, benign public https URL (no permissionDecision set)", async () => {
  const hook = createWebFetchGuardHook(webFetchDenial(["staged-secret-value-123456"]));
  const output = (await hook(
    preToolUseInput("https://api.example.com/v1/docs"),
    "tool-use-1",
    { signal: new AbortController().signal },
  )) as SyncOutput;

  assert.equal(output.hookSpecificOutput, undefined);
});

test("createWebFetchGuardHook: denies a missing/non-string url (fail-closed)", async () => {
  const hook = createWebFetchGuardHook(webFetchDenial([]));
  const input: PreToolUseHookInput = {
    session_id: "s1",
    transcript_path: "/tmp/transcript.jsonl",
    cwd: "/workspace",
    hook_event_name: "PreToolUse",
    tool_name: "WebFetch",
    tool_input: {},
    tool_use_id: "tool-use-1",
  };
  const output = (await hook(input, "tool-use-1", {
    signal: new AbortController().signal,
  })) as SyncOutput;

  assert.equal(output.hookSpecificOutput?.permissionDecision, "deny");
});

test("createWebFetchGuardHook: ignores non-PreToolUse hook events", async () => {
  const hook = createWebFetchGuardHook(webFetchDenial(["staged-secret-value-123456"]));
  const output = await hook(
    { session_id: "s1", transcript_path: "/tmp/t.jsonl", cwd: "/workspace", hook_event_name: "PostToolUse" } as never,
    "tool-use-1",
    { signal: new AbortController().signal },
  );
  assert.deepEqual(output, {});
});

test("createWebFetchGuardHook: ignores PreToolUse calls for tools other than WebFetch", async () => {
  const hook = createWebFetchGuardHook(webFetchDenial(["staged-secret-value-123456"]));
  const input: PreToolUseHookInput = {
    session_id: "s1",
    transcript_path: "/tmp/transcript.jsonl",
    cwd: "/workspace",
    hook_event_name: "PreToolUse",
    tool_name: "Bash",
    tool_input: { command: "echo staged-secret-value-123456" },
    tool_use_id: "tool-use-2",
  };
  const output = await hook(input, "tool-use-2", { signal: new AbortController().signal });
  assert.deepEqual(output, {});
});
