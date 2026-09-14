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

// webfetch_guard.ts — SSRF + secret-leak guard for the coding-agent runner's
// WebFetch tool (D9 follow-on to websearch_dlp.ts's WebSearch DLP gate).
//
// WebFetch gives the agent arbitrary outbound HTTPS access to fetch API/SDK
// docs and spec URLs — unlike WebSearch (server-side, model-API-fulfilled),
// WebFetch actually dials a URL the agent (or an untrusted page/prompt it
// read) chose. That makes it a genuine SSRF vector: a malicious or confused
// agent could be steered to fetch the pod's cloud-metadata endpoint, an
// internal cluster service, or a URL carrying a staged secret value to an
// external host as exfiltration. This module gates it with a PreToolUse hook
// — the same mechanism websearch_dlp.ts uses and for the same reason (see
// that module's doc comment): it is the pre-dispatch control point.
//
// The SSRF address classes mirror
// services/aep-api/internal/feature/artifacts/spec_collect.go's
// FetchSpecFromURL (Go, net.IP-based) — ported here to TypeScript because
// this hook only ever sees the literal `tool_input.url` string pre-dispatch
// (there is no dial-time hook to intercept a resolved IP), so the guard
// necessarily works at the URL/hostname level: scheme + literal-IP
// classification + a fixed internal-hostname denylist (dotless single-label
// hosts and the known cluster suffixes). It does NOT resolve DNS names, and —
// unlike the Go BFF fetcher (spec_collect.go), which has a DNS-rebinding-safe
// dialer that re-checks the RESOLVED IP at connect time — the SDK's WebFetch
// has NO connect-time guard. So a public-looking hostname whose A record points
// at an internal IP is allowed here and then dialed. That residual (and the
// unrestricted egress available via Bash) is closed only by an egress
// NetworkPolicy on the runner pod, which is the real network boundary; this
// hook is best-effort defense-in-depth, not an airtight SSRF gate. Redirects
// ARE covered: WebFetch does not auto-follow a cross-host 3xx — it emits a
// "REDIRECT DETECTED" message and the model must issue a NEW WebFetch call for
// the redirect target, which re-enters this hook. On any parse ambiguity, this
// hook denies — fail-closed is mandatory for a security-critical gate.

import type { HookCallback, PreToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";

// ---- IPv4 classification ---------------------------------------------------

export interface AddressCheck {
  blocked: boolean;
  reason?: string;
}

/**
 * classifyIPv4 mirrors spec_collect.go's combined
 * `!IsGlobalUnicast() || IsPrivate() || cgnat || nat64` posture for IPv4,
 * plus a conservative catch-all (multicast, Class E/reserved, the
 * 255.255.255.255 broadcast) so anything that is not an ordinary routable
 * public unicast address is blocked, per the task's "otherwise not a
 * global-unicast public address" clause.
 */
export function classifyIPv4(octets: readonly [number, number, number, number]): AddressCheck {
  const [a, b] = octets;
  if (a === 0) {
    return { blocked: true, reason: "unspecified/this-network IPv4 address" };
  }
  if (a === 127) {
    return { blocked: true, reason: "IPv4 loopback address" };
  }
  if (a === 10) {
    return { blocked: true, reason: "RFC1918 private IPv4 address (10.0.0.0/8)" };
  }
  if (a === 172 && b >= 16 && b <= 31) {
    return { blocked: true, reason: "RFC1918 private IPv4 address (172.16.0.0/12)" };
  }
  if (a === 192 && b === 168) {
    return { blocked: true, reason: "RFC1918 private IPv4 address (192.168.0.0/16)" };
  }
  if (a === 169 && b === 254) {
    return { blocked: true, reason: "IPv4 link-local address (169.254.0.0/16, incl. cloud metadata)" };
  }
  if (a === 100 && b >= 64 && b <= 127) {
    return { blocked: true, reason: "CGNAT IPv4 address (100.64.0.0/10)" };
  }
  if (a >= 224 && a <= 239) {
    return { blocked: true, reason: "IPv4 multicast address" };
  }
  if (a >= 240) {
    return { blocked: true, reason: "reserved/broadcast IPv4 address" };
  }
  return { blocked: false };
}

function parseIPv4(hostname: string): [number, number, number, number] | null {
  const parts = hostname.split(".");
  if (parts.length !== 4) return null;
  const octets: number[] = [];
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part)) return null;
    const n = Number(part);
    if (n > 255) return null;
    octets.push(n);
  }
  return octets as [number, number, number, number];
}

// ---- IPv6 classification ---------------------------------------------------

// NAT64 Well-Known Prefix (RFC 6052, 64:ff9b::/96). Mirrors nat64Net in
// spec_collect.go — a DNS64 resolver in a NAT64 cluster can synthesize a
// 64:ff9b:: AAAA for an attacker domain that then routes to an embedded
// IPv4, including link-local cloud metadata or the pod/service RFC1918
// range; block the whole prefix rather than trying to unpack the embedded
// address (matching the Go fetcher's blanket range check).
const NAT64_PREFIX = parseIPv6ToBigInt("64:ff9b::") ?? 0n;

/**
 * parseIPv6ToBigInt parses a bracket-free, zone-id-free IPv6 address (the
 * form `URL.hostname` already normalizes to — see isSsrfUrl) into its
 * 128-bit value. Returns null for anything that doesn't parse as exactly
 * eight 16-bit groups (after expanding at most one `::` run).
 */
export function parseIPv6ToBigInt(addr: string): bigint | null {
  if (addr.includes(".")) return null; // WHATWG host serialization never emits this for IPv6
  const doubleColonIdx = addr.indexOf("::");
  let groups: string[];
  if (doubleColonIdx !== -1) {
    if (addr.indexOf("::", doubleColonIdx + 1) !== -1) return null; // more than one "::"
    const head = addr.slice(0, doubleColonIdx);
    const tail = addr.slice(doubleColonIdx + 2);
    const headParts = head === "" ? [] : head.split(":");
    const tailParts = tail === "" ? [] : tail.split(":");
    const missing = 8 - headParts.length - tailParts.length;
    if (missing < 0) return null;
    groups = [...headParts, ...new Array(missing).fill("0"), ...tailParts];
  } else {
    groups = addr.split(":");
  }
  if (groups.length !== 8) return null;
  let value = 0n;
  for (const g of groups) {
    if (!/^[0-9a-fA-F]{1,4}$/.test(g)) return null;
    value = (value << 16n) | BigInt(parseInt(g, 16));
  }
  return value;
}

// extractEmbeddedIPv4 unpacks the low 32 bits of an IPv4-mapped
// (::ffff:0:0/96) or (deprecated) IPv4-compatible (::0.0.0.0/96) IPv6
// address. Mirrors Go's automatic ip.To4() unwrap inside IsPrivate /
// IsGlobalUnicast / IsLoopback — without this, `::ffff:169.254.169.254`
// would sail past pure-IPv6-range checks straight to the cloud metadata
// endpoint.
function extractEmbeddedIPv4(value: bigint): number | null {
  const top96 = value >> 32n;
  if (top96 === 0xffffn) return Number(value & 0xffffffffn); // IPv4-mapped ::ffff:a.b.c.d
  if (top96 === 0n && value !== 0n && value !== 1n) return Number(value & 0xffffffffn); // IPv4-compatible (deprecated)
  return null;
}

export function classifyIPv6(value: bigint): AddressCheck {
  if (value === 0n) {
    return { blocked: true, reason: "unspecified IPv6 address (::)" };
  }
  if (value === 1n) {
    return { blocked: true, reason: "IPv6 loopback address (::1)" };
  }
  const first16 = Number((value >> 112n) & 0xffffn);
  if ((first16 & 0xffc0) === 0xfe80) {
    return { blocked: true, reason: "IPv6 link-local address (fe80::/10)" };
  }
  if ((first16 & 0xfe00) === 0xfc00) {
    return { blocked: true, reason: "IPv6 unique-local/private address (fc00::/7)" };
  }
  if ((first16 & 0xff00) === 0xff00) {
    return { blocked: true, reason: "IPv6 multicast address (ff00::/8)" };
  }
  if ((value >> 32n) === (NAT64_PREFIX >> 32n)) {
    return { blocked: true, reason: "NAT64 well-known-prefix address (64:ff9b::/96)" };
  }
  const embedded = extractEmbeddedIPv4(value);
  if (embedded !== null) {
    const a = (embedded >>> 24) & 0xff;
    const b = (embedded >>> 16) & 0xff;
    const c = (embedded >>> 8) & 0xff;
    const d = embedded & 0xff;
    const ipv4Check = classifyIPv4([a, b, c, d]);
    if (ipv4Check.blocked) {
      return { blocked: true, reason: `IPv4-embedded IPv6 address (${ipv4Check.reason})` };
    }
  }
  return { blocked: false };
}

// ---- URL-level SSRF predicate -----------------------------------------------

const INTERNAL_HOSTNAME_SUFFIXES = [".local", ".internal", ".svc", ".cluster.local"];

/**
 * isSsrfUrl is the pure SSRF predicate: parse `rawUrl`, then deny unless it
 * is an absolute `https:` URL whose host is neither a literal non-public IP
 * (loopback/private/link-local/CGNAT/NAT64/unspecified/multicast/reserved,
 * IPv4 or IPv6, including IPv4-mapped IPv6) nor a well-known internal
 * hostname (`localhost`, or a `.local` / `.internal` / `.svc` /
 * `.cluster.local` suffix). Any parse failure or ambiguity denies —
 * fail-closed is mandatory here. Exported standalone for unit testing
 * without the hook plumbing.
 */
export function isSsrfUrl(rawUrl: string): AddressCheck {
  let parsed: URL;
  try {
    parsed = new URL(rawUrl);
  } catch {
    return { blocked: true, reason: "URL failed to parse" };
  }
  if (parsed.protocol !== "https:") {
    return { blocked: true, reason: `non-https scheme (${parsed.protocol || "unknown"})` };
  }
  const hostname = parsed.hostname;
  if (!hostname) {
    return { blocked: true, reason: "URL has no host" };
  }
  // Strip trailing dot(s) once, right after lowercasing: a trailing-dot FQDN
  // (e.g. "metadata.google.internal.") is DNS-equivalent to its bare form but
  // would otherwise slip past the suffix/localhost denylist below (`endsWith`
  // no longer matches). Harmless to literal IPs — WHATWG URL parsing already
  // strips a literal IP's trailing dot before .hostname is read — and to
  // bracketed IPv6 hosts, which never contain a ".".
  const lowerHost = hostname.toLowerCase().replace(/\.+$/, "");

  if (lowerHost.startsWith("[") && lowerHost.endsWith("]")) {
    const addr = lowerHost.slice(1, -1);
    const value = parseIPv6ToBigInt(addr);
    if (value === null) {
      return { blocked: true, reason: "unparseable IPv6 literal host" };
    }
    return classifyIPv6(value);
  }

  const ipv4 = parseIPv4(lowerHost);
  if (ipv4) {
    return classifyIPv4(ipv4);
  }

  if (
    lowerHost === "localhost" ||
    INTERNAL_HOSTNAME_SUFFIXES.some((suffix) => lowerHost.endsWith(suffix))
  ) {
    return { blocked: true, reason: `internal hostname (${lowerHost})` };
  }
  // A single-label (dotless) host is never a public FQDN — public names always
  // carry a TLD dot. Inside the cluster a bare name resolves via resolv.conf
  // search domains to an internal service (e.g. "kubernetes" → the API server,
  // "metadata" → a cloud metadata alias). Deny it as defense-in-depth. Literal
  // IPs are already classified above, so they never reach here. NB: partial
  // in-cluster names that DO carry a dot (`<svc>.<ns>`, `kubernetes.default`)
  // still slip past this string-level check — the egress NetworkPolicy is the
  // airtight boundary for those.
  if (!lowerHost.includes(".")) {
    return { blocked: true, reason: `single-label internal hostname (${lowerHost})` };
  }

  return { blocked: false };
}

export function ssrfDenialMessage(reason: string): string {
  return (
    `WebFetch URL blocked: ${reason}. Only public https URLs to external hosts are ` +
    "allowed; internal, private, loopback, link-local, and cloud-metadata addresses can never be fetched."
  );
}

// ---- Secret-in-URL predicate ------------------------------------------------

export interface UrlSecretCheck {
  denied: boolean;
  message?: string;
}

export const WEBFETCH_SECRET_DENIAL_MESSAGE =
  "WebFetch URL blocked: it contains a staged secret value. Retry without the value — " +
  "name the SDK, API, or technology only; never embed a credential or config value in a fetched URL.";

/**
 * checkUrlForSecret is the pure DLP predicate, analogous to
 * websearch_dlp.ts's checkWebSearchQuery: deny iff `url` — or its
 * percent-decoded form — contains any of `secrets` as a substring. `secrets`
 * is expected to be `stagedSecretValues(childEnv)` — the same
 * deny-by-default candidate list websearch_dlp.ts computes, reused rather
 * than duplicated.
 *
 * A raw substring match alone is bypassable: a secret containing
 * URL-reserved characters (`/ + = :` space, etc.) can be percent-encoded in
 * the URL and still land at the same destination, evading a literal
 * `url.includes(secret)` check. So this also tests the decoded haystack.
 * Matching stays case-sensitive — secrets are case-sensitive.
 */
export function checkUrlForSecret(url: string, secrets: readonly string[]): UrlSecretCheck {
  const haystacks = [url];
  try {
    haystacks.push(decodeURIComponent(url));
  } catch {
    // Malformed percent-encoding: keep the raw haystack only.
  }
  for (const secret of secrets) {
    if (secret && haystacks.some((haystack) => haystack.includes(secret))) {
      return { denied: true, message: WEBFETCH_SECRET_DENIAL_MESSAGE };
    }
  }
  return { denied: false };
}

// ---- PreToolUse hook ---------------------------------------------------

function isPreToolUseInput(input: unknown): input is PreToolUseHookInput {
  return (
    typeof input === "object" &&
    input !== null &&
    (input as { hook_event_name?: unknown }).hook_event_name === "PreToolUse"
  );
}

function denyOutput(message: string) {
  return {
    hookSpecificOutput: {
      hookEventName: "PreToolUse" as const,
      permissionDecision: "deny" as const,
      permissionDecisionReason: message,
    },
  };
}

/**
 * The platform's WebFetch rule as the runtime PORT states one: a URL in, the
 * REASON to deny it, or null to allow. Both halves of the gate in their
 * established order — SSRF first, then the secret-in-URL check.
 *
 * Fail-closed: an unparseable URL, a non-https scheme, any non-public
 * literal-IP host, any internal hostname, or a URL containing a staged
 * secret all deny. Only a well-formed `https:` URL to a public, non-secret
 * host is allowed through.
 *
 * `secrets` is the same `stagedSecretValues(childEnv)` list `webSearchDenial`
 * takes — one source of truth for "what's secret in this run".
 */
export function webFetchDenial(secrets: readonly string[]): (url: string) => string | null {
  return (url) => {
    const ssrfCheck = isSsrfUrl(url);
    if (ssrfCheck.blocked) {
      return ssrfDenialMessage(ssrfCheck.reason ?? "URL failed SSRF validation");
    }
    const secretCheck = checkUrlForSecret(url, secrets);
    if (secretCheck.denied) {
      return secretCheck.message ?? WEBFETCH_SECRET_DENIAL_MESSAGE;
    }
    return null;
  };
}

/**
 * createWebFetchGuardHook builds the PreToolUse HookCallback that gates
 * WebFetch. Register it under `hooks.PreToolUse` with `matcher: "WebFetch"`
 * alongside websearch_dlp.ts's WebSearch entry.
 *
 * It takes the DECISION, not the secrets — see `webFetchDenial` above and
 * `createWebSearchDlpHook`: the rule is the platform's, the enforcement
 * mechanism is one runtime's.
 */
export function createWebFetchGuardHook(deny: (url: string) => string | null): HookCallback {
  return async (input) => {
    if (!isPreToolUseInput(input) || input.tool_name !== "WebFetch") {
      return {};
    }
    const toolInput = input.tool_input as { url?: unknown } | undefined;
    const url = typeof toolInput?.url === "string" ? toolInput.url : "";
    const reason = deny(url);
    if (reason !== null) {
      return denyOutput(reason);
    }
    return {};
  };
}
