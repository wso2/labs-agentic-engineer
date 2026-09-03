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
 * WriteLedger — the turn's record of file writes, so each one is applied EXACTLY
 * ONCE and its verdict reaches the wire the moment that call's arguments close.
 *
 * WHY THIS EXISTS. The AI SDK does not execute a tool when its call arrives: it
 * queues every call of a step and runs them all at `model-call-end`, i.e. after
 * the whole assistant message has streamed (ai@7 `runToolsTransformation`). For
 * a step that batches five `addFile`s — how a design turn writes — that means no
 * `tool-result` for file 1 until file 5's body has finished streaming, minutes
 * later. Consumers that (correctly) refuse to claim a write before the bundle
 * has ruled on it were left with nothing to show: the console painted four
 * finished files as verdict-less pending rings for the length of the batch.
 *
 * The verdict does not actually depend on anything the SDK is waiting for — a
 * bundle op is a pure function of the bundle and the call's arguments, and for a
 * file tool the arguments ARE the body, so `tool-input-end` is the moment the op
 * can run. So it runs there, and its `tool-result` frame rides that call's own
 * `tool-call` instead of the step's tail.
 *
 * HOW IT STAYS HONEST:
 *  - ONE apply per `toolCallId`. `apply()` memoises the `OpResult`, so the SDK's
 *    later `execute()` returns the SAME verdict rather than re-applying (a second
 *    `addFile` would answer ALREADY_EXISTS and the model would read a failure for
 *    a write that succeeded).
 *  - The arguments are validated with the tool's OWN schema before the op runs,
 *    so a call the SDK would reject as invalid is never applied early; its frames
 *    stay exactly as the SDK produced them.
 *  - Exactly one `tool-result` per call on the wire: a call this ledger settled
 *    suppresses the SDK's duplicate. A call it did NOT settle is untouched, which
 *    is what makes every un-tapped path (task-plan turns, one-shot runs, evals)
 *    byte-identical to before.
 *  - Ordering is unchanged: the ops still apply in call order (a provider
 *    serialises the tool_use blocks of one message), just earlier in wall-clock.
 *
 * A turn that dies between an input-end and the SDK's execute leaves the bundle
 * holding an op the transcript has no result for. That is safe by construction:
 * such a turn emits no manifest, and D14 refuses to commit a turn without one.
 */

import type { OpResult, StreamPart } from "@aep/agent-stream";

/**
 * One file-mutation tool as the ledger needs it: validate the streamed
 * arguments, then apply them to the bundle. Registered by the tool set that owns
 * the schemas (`files.ts`) so the ledger itself knows nothing about which tools
 * exist — the validator IS the tool's `inputSchema`, never a second copy of it.
 */
export interface WriteOp {
  /** Parse the complete streamed args; `undefined` ⇒ the SDK will reject them too. */
  validate(args: unknown): unknown | undefined;
  /** Apply the validated input to the bundle and return the bundle's verdict. */
  apply(input: unknown): OpResult;
}

/** A call whose args have closed and whose op has run, awaiting its `tool-call`. */
interface Pending {
  toolName: string;
  input: unknown;
}

export class WriteLedger {
  /** toolCallId → the verdict its op produced (the apply-once record). */
  private readonly verdicts = new Map<string, OpResult>();
  /** Args accumulating for an in-flight call: id → tool + JSON buffer. */
  private readonly streaming = new Map<string, { toolName: string; buf: string }>();
  /** Applied, verdict not yet on the wire (it rides the call's `tool-call`). */
  private readonly pending = new Map<string, Pending>();
  /** Calls whose verdict this ledger emitted — the SDK's copy is a duplicate. */
  private readonly settled = new Set<string>();

  constructor(private readonly ops: Readonly<Record<string, WriteOp>>) {}

  /**
   * The tool's `execute()` path: apply this call's op, or return the verdict an
   * earlier apply already recorded for the same `toolCallId`. The only place a
   * file write reaches the bundle, whichever half of the loop gets here first.
   */
  apply(toolCallId: string | undefined, toolName: string, input: unknown): OpResult {
    const op = this.ops[toolName];
    if (!op) throw new Error(`WriteLedger: no write op registered for ${toolName}`);
    if (toolCallId !== undefined) {
      const already = this.verdicts.get(toolCallId);
      if (already) return already;
    }
    const verdict = op.apply(input);
    if (toolCallId !== undefined) this.verdicts.set(toolCallId, verdict);
    return verdict;
  }

  /**
   * The stream tap: the frames to forward in place of `part`.
   *
   *  - a file call's `tool-input-end` → the op runs (nothing extra emitted yet);
   *  - that call's `tool-call` → `[part, its tool-result]`, so the wire keeps the
   *    canonical call-then-result order rather than announcing a result for a
   *    call no consumer has seen;
   *  - the SDK's own `tool-result` for a call already settled here → `[]`.
   *
   * A call whose `tool-call` never arrives (a severed stream) is never marked
   * settled, so the SDK's frames — if any still come — are forwarded as they are.
   */
  project(part: StreamPart): StreamPart[] {
    switch (part.type) {
      case "tool-input-start":
        if (part.id && part.toolName && this.ops[part.toolName]) {
          this.streaming.set(part.id, { toolName: part.toolName, buf: "" });
        }
        break;

      case "tool-input-delta": {
        const call = part.id ? this.streaming.get(part.id) : undefined;
        if (call) call.buf += part.delta ?? "";
        break;
      }

      case "tool-input-end": {
        const id = part.id;
        const call = id ? this.streaming.get(id) : undefined;
        if (!id || !call) break;
        this.streaming.delete(id);
        const input = this.validated(call.toolName, call.buf);
        if (input === undefined) break; // invalid args — the SDK's own path owns this call
        this.apply(id, call.toolName, input);
        this.pending.set(id, { toolName: call.toolName, input });
        break;
      }

      case "tool-call": {
        const id = part.toolCallId;
        const call = id ? this.pending.get(id) : undefined;
        const verdict = id ? this.verdicts.get(id) : undefined;
        if (!id || !call || !verdict) break;
        this.pending.delete(id);
        this.settled.add(id);
        return [
          part,
          {
            type: "tool-result",
            toolCallId: id,
            toolName: call.toolName,
            input: call.input,
            output: verdict,
          },
        ];
      }

      case "tool-result":
        // Its verdict is already on the wire; a second frame would re-settle a
        // card the consumer has finished with.
        if (part.toolCallId && this.settled.has(part.toolCallId)) return [];
        break;
    }
    return [part];
  }

  /** Validate one call's buffered args with the tool's own schema. */
  private validated(toolName: string, buf: string): unknown | undefined {
    let args: unknown;
    try {
      args = JSON.parse(buf);
    } catch {
      return undefined; // truncated / malformed — not ours to apply
    }
    return this.ops[toolName]?.validate(args);
  }
}

/**
 * Wrap a turn's `onEvent` so every write settles at its own call. ONE definition
 * of the wiring, shared by the turn orchestration and the tests that pin the
 * frame order — a second hand-rolled wrap is how the two would drift.
 */
export function tapWrites(
  writes: WriteLedger,
  onEvent: (part: StreamPart) => void,
): (part: StreamPart) => void {
  return (part) => {
    for (const out of writes.project(part)) onEvent(out);
  };
}
