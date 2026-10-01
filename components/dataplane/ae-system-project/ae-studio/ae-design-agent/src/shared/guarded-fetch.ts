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
 * The call-time host guard for model requests (SSRF). A model connection's base
 * URL is typed by an org admin and checked by aep-api when it is saved, but DNS
 * can change after the save (rebinding) and this service calls the URL on every
 * turn, so the check is repeated here, at the socket.
 *
 * The guard lives in the connector of a dedicated undici dispatcher: every
 * connection resolves its host once, refuses the WHOLE answer if any address is
 * non-public, and hands the checked addresses to the socket as its lookup — so
 * the socket dials exactly what was checked, never a second resolution. An IP
 * literal skips the lookup (Node does not resolve one), so the connector checks
 * it directly. aep-api's `platform/netguard` refuses the same address set
 * (public unicast only; CGNAT and NAT64 refused).
 *
 * Only the model's fetch uses this dispatcher; the process's global dispatcher
 * (collab, MCP, telemetry) is untouched.
 */

import type { LookupAddress } from "node:dns";
import { lookup as dnsLookup } from "node:dns/promises";
import { BlockList, isIP, type LookupFunction } from "node:net";
import {
  Agent,
  buildConnector,
  fetch as undiciFetch,
  type RequestInfo as UndiciRequestInfo,
  type RequestInit as UndiciRequestInit,
} from "undici";

/** The fetch signature the AI SDK providers take. */
type FetchFunction = typeof globalThis.fetch;

/** Resolves a host name to every address it answers (a DNS lookup with `all`). */
type Resolve = (hostname: string) => Promise<LookupAddress[]>;

/**
 * Addresses a model request may never reach: everything that is not public
 * unicast. IPv4-mapped IPv6 (`::ffff:a.b.c.d`) matches the IPv4 rules, because
 * `BlockList` checks the embedded address.
 */
const NON_PUBLIC = new BlockList();
// IPv4
NON_PUBLIC.addSubnet("0.0.0.0", 8, "ipv4"); // "this network", incl. unspecified
NON_PUBLIC.addSubnet("10.0.0.0", 8, "ipv4"); // private (RFC 1918)
NON_PUBLIC.addSubnet("100.64.0.0", 10, "ipv4"); // CGNAT shared space (RFC 6598)
NON_PUBLIC.addSubnet("127.0.0.0", 8, "ipv4"); // loopback
NON_PUBLIC.addSubnet("169.254.0.0", 16, "ipv4"); // link-local, incl. cloud metadata
NON_PUBLIC.addSubnet("172.16.0.0", 12, "ipv4"); // private (RFC 1918)
NON_PUBLIC.addSubnet("192.168.0.0", 16, "ipv4"); // private (RFC 1918)
NON_PUBLIC.addSubnet("224.0.0.0", 4, "ipv4"); // multicast
NON_PUBLIC.addSubnet("240.0.0.0", 4, "ipv4"); // reserved, incl. broadcast
// IPv6
NON_PUBLIC.addAddress("::", "ipv6"); // unspecified
NON_PUBLIC.addAddress("::1", "ipv6"); // loopback
NON_PUBLIC.addSubnet("64:ff9b::", 96, "ipv6"); // NAT64 well-known prefix (RFC 6052)
NON_PUBLIC.addSubnet("64:ff9b:1::", 48, "ipv6"); // NAT64 local-use prefix (RFC 8215)
NON_PUBLIC.addSubnet("fc00::", 7, "ipv6"); // unique local (ULA)
NON_PUBLIC.addSubnet("fe80::", 10, "ipv6"); // link-local
NON_PUBLIC.addSubnet("ff00::", 8, "ipv6"); // multicast

/** Whether `address` (an IP literal) is public unicast. Anything unparseable is not. */
export function isPublicAddress(address: string): boolean {
  const family = isIP(address);
  if (family === 0) return false;
  return !NON_PUBLIC.check(address, family === 4 ? "ipv4" : "ipv6");
}

/**
 * A model request refused because its host is, or resolves to, a non-public
 * address. Names the host as typed and never a resolved address: echoing the
 * answer would turn the error into an oracle for internal DNS.
 */
export class HostRefusedError extends Error {
  constructor(readonly hostname: string) {
    super(`refusing to connect to ${hostname}: it resolves to a non-public address`);
    this.name = "HostRefusedError";
  }
}

/**
 * A model request refused because its host answered with a redirect. Not
 * followed: `fetch` would forward `x-api-key` to the new host.
 */
export class RedirectRefusedError extends Error {
  constructor(readonly hostname: string) {
    super(`refusing to follow a redirect from ${hostname}`);
    this.name = "RedirectRefusedError";
  }
}

/** The `HostRefusedError` a failed fetch carries somewhere in its cause chain. */
function hostRefusal(err: unknown): HostRefusedError | undefined {
  for (let e: unknown = err, depth = 0; e && depth < 8; e = (e as { cause?: unknown }).cause, depth++) {
    if (e instanceof HostRefusedError) return e;
  }
  return undefined;
}

/**
 * A socket lookup that resolves once, validates every answer, and returns only
 * what it validated. Answers in the shape the socket asked for: `net.connect`
 * asks for `all` addresses when it races families (the default since Node 20).
 */
function guardedLookup(resolve: Resolve, permit: (address: string) => boolean): LookupFunction {
  return (hostname, options, callback) => {
    resolve(hostname).then(
      (answers) => {
        if (answers.length === 0 || !answers.every((a) => permit(a.address))) {
          callback(new HostRefusedError(hostname), "");
          return;
        }
        const wanted = options.family === 4 || options.family === 6 ? options.family : 0;
        const matching = wanted ? answers.filter((a) => a.family === wanted) : answers;
        if (options.all) {
          callback(null, matching);
          return;
        }
        const first = matching[0];
        if (!first) {
          callback(Object.assign(new Error(`no IPv${wanted} address for ${hostname}`), { code: "ENOTFOUND" }), "");
          return;
        }
        callback(null, first.address, first.family);
      },
      (err: NodeJS.ErrnoException) => callback(err, ""),
    );
  };
}

interface GuardOptions {
  /** How a host is resolved. Defaults to the system resolver. */
  resolve?: Resolve;
  /** Which addresses may be dialled. Defaults to `isPublicAddress`. */
  permit?: (address: string) => boolean;
}

/**
 * A fetch whose every connection passes the host guard. Redirects are refused
 * rather than followed: `fetch` drops `Authorization` on a cross-origin hop but
 * forwards `x-api-key`, so a followed redirect would carry the org's key to a
 * host nobody checked.
 *
 * Both refusals are thrown as themselves (`HostRefusedError`,
 * `RedirectRefusedError`), never as undici's `TypeError("fetch failed")`: the AI
 * SDK retries that as a network error — six times, over two minutes, re-resolving
 * the host each time — and neither answer changes on a retry.
 *
 * The AI SDK calls it with a string URL; a `Request` object from the global
 * fetch is not a valid input for undici's own fetch.
 */
export function createGuardedFetch({ resolve, permit = isPublicAddress }: GuardOptions = {}): FetchFunction {
  const socketConnect = buildConnector({
    lookup: guardedLookup(resolve ?? ((host) => dnsLookup(host, { all: true })), permit),
  });
  const dispatcher = new Agent({
    connect: (options, callback) => {
      if (isIP(options.hostname) !== 0 && !permit(options.hostname)) {
        callback(new HostRefusedError(options.hostname), null);
        return;
      }
      socketConnect(options, callback);
    },
  });
  // undici's own fetch with its own dispatcher: Node's global fetch bundles a
  // different undici major, and a dispatcher is not portable across the two.
  // The casts cross that boundary: the two majors' types describe the same
  // fetch but are not assignable to each other.
  return (async (input: Parameters<FetchFunction>[0], init?: Parameters<FetchFunction>[1]) => {
    let res: Awaited<ReturnType<typeof undiciFetch>>;
    try {
      res = await undiciFetch(input as UndiciRequestInfo, {
        ...(init as unknown as UndiciRequestInit),
        dispatcher,
        redirect: "manual",
      });
    } catch (err) {
      throw hostRefusal(err) ?? err;
    }
    if (res.status >= 300 && res.status < 400 && res.headers.has("location")) {
      await res.body?.cancel();
      throw new RedirectRefusedError(new URL(res.url || String(input)).host);
    }
    return res;
  }) as unknown as FetchFunction;
}

/**
 * The process-wide guarded fetch every model provider uses. One dispatcher, so
 * connections pool across turns.
 */
export const guardedFetch: FetchFunction = createGuardedFetch();
