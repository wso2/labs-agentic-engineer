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

// The in-process ToolsSocket: what a test (or the playground) drives a turn
// with instead of ae-studio-tools. It answers the MCP JSON-RPC surface with
// the eleven design tools, records every tool call and usage record, serves
// lookups from a fixed project table (recording each, and answering
// ref_not_found for a listed missing `at`), and can be told to fail or hold the
// next usage hand-overs so the outbox's retry paths are testable.

import type { ProjectSnapshot, SkillsSnapshot, ToolsSocket, TurnRecord } from "./client.js";
import { ToolsSocketError } from "./client.js";

/**
 * The eleven design tools ae-studio-tools answers on `tools/list`, whatever
 * aep-api serves (aep-api `mcpdiscovery/mcp_tools.go`, pinned in
 * ae-studio-tools `internal/mcp`).
 */
export const DESIGN_TOOL_NAMES = [
  "list_external_resources",
  "get_external_resource_schema",
  "list_org_endpoints",
  "list_org_component_endpoints",
  "list_platform_resource_types",
  "list_groups",
  "get_remote_git_file_contents",
  "search_remote_git_code",
  "validate_openapi_spec",
  "fetch_openapi_spec",
  "slice_openapi_spec",
] as const;

export interface FakeToolCall {
  name: string;
  arguments: unknown;
}

export interface FakeToolsSocketOptions {
  /** Known projects by name; any other name looks up as `null`. */
  projects?: Record<string, ProjectSnapshot>;
  /** `at` values that name no commit: `lookup` throws 404 `ref_not_found` for them, as the socket does. */
  missingRefs?: string[];
  /** The skills snapshot's sha (`skills()`). */
  skillsSha?: string;
  /** The room token `roomToken()` answers. */
  roomToken?: string;
  /** A tool's text result; defaults to `{"tool": <name>}`. */
  callTool?: (call: FakeToolCall) => string;
}

interface JsonRpcRequest {
  id?: unknown;
  method?: unknown;
  params?: { name?: unknown; arguments?: unknown };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

/** @knipkeep test seam: the in-process ToolsSocket the tests drive turns with */
export class FakeToolsSocket implements ToolsSocket {
  /** Every record `postUsage` accepted, in order. */
  readonly usage: TurnRecord[] = [];
  /** Every `tools/call`, in order. */
  readonly toolCalls: FakeToolCall[] = [];
  /** Every `lookup` call, in order; `at` only when given. */
  readonly lookups: Array<{ project: string; at?: string }> = [];
  /** Every `postUsage` call, accepted or not. */
  usageAttempts = 0;

  private readonly projects: Record<string, ProjectSnapshot>;
  private readonly missingRefs: ReadonlySet<string>;
  private readonly skillsSha: string;
  private readonly token: string;
  private readonly callTool: (call: FakeToolCall) => string;
  private failures = 0;
  private failStatus = 503;
  private held: Promise<void> | null = null;
  private lookupHeld: Promise<void> | null = null;

  constructor(opts: FakeToolsSocketOptions = {}) {
    this.projects = opts.projects ?? {};
    this.missingRefs = new Set(opts.missingRefs ?? []);
    this.skillsSha = opts.skillsSha ?? "skills-fake";
    this.token = opts.roomToken ?? "room-token-fake";
    this.callTool = opts.callTool ?? ((call) => JSON.stringify({ tool: call.name }));
  }

  /** The next `count` `postUsage` calls fail: 503 (transient), or 400 when `permanent`. */
  failUsage(count: number, opts: { permanent?: boolean } = {}): void {
    this.failures = count;
    this.failStatus = opts.permanent ? 400 : 503;
  }

  /** `postUsage` calls wait until `release()`; nothing is recorded meanwhile. */
  holdUsage(): { release(): void } {
    let release!: () => void;
    this.held = new Promise<void>((resolve) => (release = resolve));
    return {
      release: () => {
        this.held = null;
        release();
      },
    };
  }

  /** `lookup` calls wait until `release()` (a cold tools sidecar). */
  holdLookups(): { release(): void } {
    let release!: () => void;
    this.lookupHeld = new Promise<void>((resolve) => (release = resolve));
    return {
      release: () => {
        this.lookupHeld = null;
        release();
      },
    };
  }

  readonly mcpFetch = (async (_input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    const req = JSON.parse(String(init?.body ?? "{}")) as JsonRpcRequest;
    if (req.id === undefined) return new Response(null, { status: 202 });
    if (req.method === "initialize") {
      return json({ jsonrpc: "2.0", id: req.id, result: { protocolVersion: "2024-11-05", capabilities: { tools: {} } } });
    }
    if (req.method === "tools/list") {
      const tools = DESIGN_TOOL_NAMES.map((name) => ({
        name,
        description: name,
        inputSchema: { type: "object", properties: {} },
      }));
      return json({ jsonrpc: "2.0", id: req.id, result: { tools } });
    }
    if (req.method === "tools/call") {
      const call = { name: String(req.params?.name), arguments: req.params?.arguments ?? {} };
      if (!(DESIGN_TOOL_NAMES as readonly string[]).includes(call.name)) {
        return json({ jsonrpc: "2.0", id: req.id, error: { code: -32602, message: `unknown tool: ${call.name}` } });
      }
      this.toolCalls.push(call);
      return json({ jsonrpc: "2.0", id: req.id, result: { content: [{ type: "text", text: this.callTool(call) }] } });
    }
    return json({ jsonrpc: "2.0", id: req.id, error: { code: -32601, message: "method not found" } });
  }) as typeof fetch;

  async roomToken(): Promise<string> {
    return this.token;
  }

  async postUsage(r: TurnRecord): Promise<void> {
    this.usageAttempts++;
    if (this.held) await this.held;
    if (this.failures > 0) {
      this.failures--;
      throw new ToolsSocketError(this.failStatus === 400 ? "invalid_record" : "aep_api_unavailable", this.failStatus);
    }
    this.usage.push(r);
  }

  async lookup(project: string, at?: string): Promise<ProjectSnapshot | null> {
    this.lookups.push(at === undefined ? { project } : { project, at });
    if (this.lookupHeld) await this.lookupHeld;
    const known = this.projects[project];
    // The project resolves first, as on the socket: an unknown project is null whatever `at` says.
    if (known && at !== undefined && this.missingRefs.has(at)) {
      throw new ToolsSocketError("ref_not_found", 404, "at names no commit of the repository");
    }
    return known ? { ...known, references: [...known.references] } : null;
  }

  async skills(): Promise<SkillsSnapshot> {
    return { skillsSha: this.skillsSha };
  }
}
