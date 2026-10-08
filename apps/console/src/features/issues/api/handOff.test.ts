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

import createClient from "openapi-fetch";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { paths } from "../../../generated/aep-api";

// What handing an issue to the coding agent puts on the wire, and how it
// reads the server's refusals: a 409 (no deployed version, or the issue was
// closed meanwhile) is said in the server's words; anything else is one plain
// failure.

const post = vi.fn<(path: string, init: Record<string, unknown>) => Promise<unknown>>();
vi.mock("../../../api/client", () => ({
  client: { POST: (path: string, init: Record<string, unknown>) => post(path, init) },
}));

const { HAND_OFF_FAILED, HandOffRefusedError, handToCodingAgent } = await import("./handOff");

const NO_VERSION = "Deploy a version first: the coding agent works in a deployed version's milestone.";

function refused(status: number, error: unknown) {
  post.mockResolvedValueOnce({ data: undefined, error, response: { status } });
}

describe("handToCodingAgent", () => {
  beforeEach(() => post.mockReset());

  it("promotes the issue with the chosen component", async () => {
    post.mockResolvedValueOnce({ data: undefined, error: undefined, response: { status: 202 } });
    await handToCodingAgent("shop", 7, "api");
    expect(post).toHaveBeenCalledWith("/projects/{projectName}/tasks/{issueNumber}/promote-from-issue", {
      params: { path: { projectName: "shop", issueNumber: 7 } },
      body: { componentName: "api" },
      parseAs: "text",
    });
  });

  it("reads any 2xx as handed over, whatever body it carries", async () => {
    // A real client over a server whose 202 carries a body that is not JSON.
    const real = createClient<paths>({
      baseUrl: "http://aep.test/api/v1",
      fetch: () => Promise.resolve(new Response("Accepted", { status: 202, headers: { "Content-Type": "application/json" } })),
    });
    post.mockImplementationOnce((path, init) => real.POST(path as never, init as never));
    await expect(handToCodingAgent("shop", 7, "api")).resolves.toBeUndefined();
  });

  it("reads a 409 as a refusal, in the server's words", async () => {
    refused(409, { code: "conflict", message: NO_VERSION });
    const err = await handToCodingAgent("shop", 7, "api").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HandOffRefusedError);
    expect((err as Error).message).toBe(NO_VERSION);
  });

  it("says it could not when a 409 carries no words", async () => {
    refused(409, {});
    await expect(handToCodingAgent("shop", 7, "api")).rejects.toThrow(HAND_OFF_FAILED);
  });

  it("reads any other refusal as one plain failure", async () => {
    refused(500, { code: "internal", message: "failed to promote" });
    const err = await handToCodingAgent("shop", 7, "api").catch((e: unknown) => e);
    expect(err).not.toBeInstanceOf(HandOffRefusedError);
    expect((err as Error).message).toBe(HAND_OFF_FAILED);
  });

  it("says the same when the request itself fails", async () => {
    post.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    await expect(handToCodingAgent("shop", 7, "api")).rejects.toThrow(HAND_OFF_FAILED);
  });
});
