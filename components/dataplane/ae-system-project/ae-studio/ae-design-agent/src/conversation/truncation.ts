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
 * A write cut off by the output limit. When a step ends on `finishReason:
 * length` in the middle of a tool call's arguments, the call never closes into
 * something that can run: for a file write that means the file body was
 * streamed and then thrown away. Left alone, the turn would still succeed and
 * its manifest would fold the draft without that file, so a truncated write is
 * reported as the turn's failure instead, naming the file when the partial
 * arguments had already named it.
 */

import type { StreamPart } from "@aep/agent-stream";

/** The tool call the output limit cut off. */
export interface CutOffCall {
  toolName: string;
  /** The `path` argument, when the partial arguments got that far. */
  path?: string;
}

/** The turn ended with a tool call cut off by the output limit. Maps to the `output_truncated` frame. */
export class OutputTruncatedError extends Error {
  readonly code = "output_truncated";
  constructor(
    readonly call: CutOffCall,
    /** The per-step output ceiling the call hit. */
    readonly maxOutputTokens: number,
  ) {
    super(
      `The model's output limit (${maxOutputTokens} tokens per step) cut off ${call.toolName}` +
        `${call.path ? ` for ${call.path}` : ""} before it finished, so nothing was written.`,
    );
    this.name = "OutputTruncatedError";
  }
}

/** A `"path": "…"` argument already streamed in full, read out of partial JSON. */
const PATH_ARG = /"path"\s*:\s*"((?:[^"\\]|\\.)*)"/;

/**
 * Watches one turn's raw stream for a step that the output limit cut off
 * mid-call: a tool call whose arguments started streaming in a step that
 * finished on `length` and never produced a result.
 */
export class TruncationWatch {
  private step = new Map<string, { toolName: string; args: string; settled: boolean }>();
  private cutOff: CutOffCall | undefined;

  observe(part: StreamPart): void {
    switch (part.type) {
      case "start-step":
        this.step = new Map();
        this.cutOff = undefined;
        return;
      case "tool-input-start":
        if (part.id) this.step.set(part.id, { toolName: part.toolName ?? "", args: "", settled: false });
        return;
      case "tool-input-delta": {
        const call = part.id ? this.step.get(part.id) : undefined;
        if (call && typeof part.delta === "string") call.args += part.delta;
        return;
      }
      case "tool-result": {
        const call = part.toolCallId ? this.step.get(part.toolCallId) : undefined;
        if (call) call.settled = true;
        return;
      }
      case "finish-step":
        if (part.finishReason === "length") this.cutOff = this.lastUnsettled();
        return;
      default:
        return;
    }
  }

  /** The call the last step's output limit cut off, if it cut one off. */
  cutOffCall(): CutOffCall | undefined {
    return this.cutOff;
  }

  private lastUnsettled(): CutOffCall | undefined {
    const unsettled = [...this.step.values()].filter((c) => !c.settled);
    const last = unsettled[unsettled.length - 1];
    if (!last) return undefined;
    const path = PATH_ARG.exec(last.args)?.[1];
    return path !== undefined ? { toolName: last.toolName, path: unescapeJson(path) } : { toolName: last.toolName };
  }
}

function unescapeJson(s: string): string {
  try {
    return JSON.parse(`"${s}"`) as string;
  } catch {
    return s;
  }
}
