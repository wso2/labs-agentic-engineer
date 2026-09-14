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

// Single-owner stdout writer for the runner's run-event NDJSON, and the one
// place the v2 envelope is stamped. All progress flows through emit(); nothing
// else in the runner writes to stdout.
//
// **The event TYPE is generated, not written here.** `RunEvent` comes from
// `packages/contracts/api/v1/openapi.yaml` through `openapi-typescript` (npm
// run gen, wired into the root `make gen`), so there is no hand-kept mirror to
// drift: the runner, aep-api's Go and the console all read one document. The
// import is deliberately TYPE-ONLY — the pod runs `tsx`, which erases types,
// and the generated file is gitignored and absent from the image, so a value
// import of it would crash the pod on startup. See runners/AGENTS.md.
//
// What this file still owns is the small amount that is NOT in the contract:
// the envelope fields a caller must not be able to forget (`v`, `seq`, `ts`),
// the default author of a line, and the scrubber walk.

import { scrubber } from "./scrubber.js";
import type { components } from "../../generated/aep-api";

/** One event on a run's live feed, exactly as the committed contract defines it. */
export type RunEvent = components["schemas"]["RunEvent"];

/** How an agent, or a backgrounded task it owns, ended. */
export type AgentStatus = components["schemas"]["AgentStatus"];

/**
 * The token usage of a turn or of the whole run, aggregate plus per-model split.
 *
 * TurnUsage, not Usage: the platform prices each model's slice against that
 * model's own rate row, and an aggregate that folded two models reports `model:
 * ""` — unpriceable on its own. See the contract's TurnUsage for that and for
 * the other trap, that the runtime's figures are cumulative across a session.
 */
export type RunEventUsage = components["schemas"]["TurnUsage"];

/** One model's slice of a turn's usage — an entry of RunEventUsage.models. */
export type RunEventModelUsage = components["schemas"]["Usage"];

/**
 * The envelope version on the wire.
 *
 * Written as a typed literal rather than read off the generated module, because
 * the generated module is types only at runtime: a value import would survive
 * `tsx`'s erasure and look for a file the image does not ship. The `RunEvent["v"]`
 * annotation is what keeps this honest — a contract that bumped to 3 fails the
 * typecheck here rather than shipping a mislabelled feed.
 */
export const RUN_EVENT_VERSION: RunEvent["v"] = 2;

/**
 * The session's top-level agent, as the contract names it.
 *
 * Attribution is ONE field in v2 — `agentId`, on every event — and the lead is
 * the literal string below rather than an absence. v1 said "absent means main",
 * which is why every consumer had to know that rule; a reader of v2 never has to
 * infer an author from anything.
 */
export const LEAD_AGENT_ID = "lead";

/**
 * What a producer hands `emit()`: the event minus the envelope this module
 * stamps, with `agentId` optional because most of the runner's own lines are
 * the lead's and repeating that on every call site is a rule someone forgets.
 * The adapter, which is the one place that knows about spawned agents, always
 * passes it.
 */
export type RunEventInput = Omit<RunEvent, "v" | "seq" | "ts" | "agentId"> & { agentId?: string };

let seqCounter = 0;

export function emit(event: RunEventInput): void {
  seqCounter += 1;
  const enriched: RunEvent = {
    v: RUN_EVENT_VERSION,
    ts: new Date().toISOString(),
    seq: seqCounter,
    agentId: LEAD_AGENT_ID,
    ...event,
  };
  process.stdout.write(JSON.stringify(scrubValue(enriched)) + "\n");
}

/**
 * Scrub each string VALUE, then serialize — never the other way round.
 *
 * Scrubbing the serialized line hands the scrubber JSON syntax as if it were
 * prose, and its header patterns end in `(\S+)`. `JSON.stringify` puts no
 * whitespace between fields, so on `…"summary":"curl -H authorization:TOK"}`
 * that `\S+` runs straight through `TOK"}` and swallows the closing quote and
 * brace: the line stops being JSON, the BFF's parser falls back to wrapping it
 * as a raw line, and the console prints the fragment. The quieter half is worse
 * — with fields after the match it eats those instead, still parses, and
 * silently drops `toolUseId`/`agentId`, so the line loses the attribution that
 * groups it under its agent.
 *
 * Values carry no JSON syntax, so a greedy match can only ever consume the
 * secret it was aimed at.
 */
// Walks the whole event rather than just its top level, so a nested string
// added to the contract later is scrubbed without anyone remembering to opt in.
function scrubValue(value: unknown): unknown {
  if (typeof value === "string") return scrubber.scrub(value);
  if (Array.isArray(value)) return value.map(scrubValue);
  if (value !== null && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value)) out[k] = scrubValue(v);
    return out;
  }
  return value;
}

export function primeScrubber(secrets: Iterable<string | undefined | null>): void {
  for (const s of secrets) scrubber.addLiteral(s ?? undefined);
}

// Test seam.
export function _resetEmitterForTesting(): void {
  seqCounter = 0;
  scrubber.reset();
}
