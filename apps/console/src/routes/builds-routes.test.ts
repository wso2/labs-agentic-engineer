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

import { describe, expect, it, vi } from "vitest";

// The route modules pull in their page components, which drag half the app
// behind them. These tests are about URL SEMANTICS — what redirects where, and
// how a param parses — so the components are stubbed out.
vi.mock("../features/builds/components/BuildsLedger", () => ({
  BuildsLedger: () => null,
}));
vi.mock("../features/builds/components/BuildDetailPage", () => ({
  BuildDetailPage: () => null,
}));
vi.mock("../features/tasks/components/TaskPage", () => ({ TaskPage: () => null }));

import { Route as buildsIndexRoute } from "./projects.$projectName.builds.index";
import { Route as buildTagRoute } from "./projects.$projectName.builds.$tag";
import { Route as taskRoute } from "./projects.$projectName.tasks.$issueNumber";

/**
 * The routing half of ADR-0021 §8 — `/builds/$tag` is a version, and
 * `/tasks/$issueNumber` is a task. TanStack cannot carry two dynamic siblings,
 * so `/builds/$issueNumber` had to go, and every old link to it has to keep
 * resolving. That is a URL contract, and it is the part of this feature most
 * likely to break silently.
 *
 * `beforeLoad` signals a redirect by THROWING, and what it throws is a
 * `Response` (307) carrying the routing options on `.options` — not a plain
 * object. Reading `.options` is what makes these assertions test the redirect
 * the router will actually perform.
 */
function redirectFrom(
  fn: ((ctx: never) => unknown) | undefined,
  ctx: unknown,
): Record<string, unknown> | null {
  try {
    (fn as (c: unknown) => unknown)?.(ctx);
    return null;
  } catch (thrown) {
    const options = (thrown as { options?: Record<string, unknown> }).options;
    if (!options) throw thrown; // a real error, not a redirect
    return options;
  }
}

describe("/projects/$projectName/builds — the ledger", () => {
  const beforeLoad = buildsIndexRoute.options.beforeLoad;

  it("sends an old ?tag= link to that version's page", () => {
    // The search param named a version, and it still resolves to one — dropping
    // it would land the reader on a list having asked for a specific build.
    const r = redirectFrom(beforeLoad, {
      params: { projectName: "demo-shop" },
      search: { tag: "v3" },
    });
    expect(r).toMatchObject({
      to: "/projects/$projectName/builds/$tag",
      params: { projectName: "demo-shop", tag: "v3" },
      replace: true,
    });
  });

  it("REPLACES rather than pushes, so Back does not bounce", () => {
    const r = redirectFrom(beforeLoad, {
      params: { projectName: "demo-shop" },
      search: { tag: "v1" },
    });
    expect(r?.replace).toBe(true);
  });

  it("stays on the ledger when no tag was asked for", () => {
    expect(
      redirectFrom(beforeLoad, { params: { projectName: "demo-shop" }, search: {} }),
    ).toBeNull();
  });

  it("ignores a tag that is not a non-empty string", () => {
    const validate = buildsIndexRoute.options.validateSearch as (
      s: Record<string, unknown>,
    ) => { tag?: string };
    expect(validate({ tag: "v2" })).toEqual({ tag: "v2" });
    expect(validate({ tag: "" })).toEqual({});
    expect(validate({ tag: 7 })).toEqual({});
    expect(validate({})).toEqual({});
  });
});

describe("/projects/$projectName/builds/$tag — a version, or an old task link", () => {
  const beforeLoad = buildTagRoute.options.beforeLoad;

  it("keeps every old /builds/<issueNumber> link working", () => {
    const r = redirectFrom(beforeLoad, {
      params: { projectName: "demo-shop", tag: "118" },
    });
    expect(r).toMatchObject({
      to: "/projects/$projectName/tasks/$issueNumber",
      params: { projectName: "demo-shop", issueNumber: 118 },
      replace: true,
    });
  });

  it("passes the issue number as a NUMBER, which the task route parses", () => {
    const r = redirectFrom(beforeLoad, {
      params: { projectName: "demo-shop", tag: "42" },
    });
    expect((r?.params as { issueNumber: unknown }).issueNumber).toBe(42);
  });

  it("treats a version tag as a version, not a number", () => {
    for (const tag of ["v1", "v10", "main"]) {
      expect(
        redirectFrom(beforeLoad, { params: { projectName: "demo-shop", tag } }),
      ).toBeNull();
    }
  });

  it("does not mistake a non-positive or fractional segment for an issue", () => {
    // These are not issue numbers, so they fall through to the version page,
    // which answers with its own "no build" state rather than redirecting into
    // a task that cannot exist.
    for (const tag of ["0", "-3", "1.5", "12abc"]) {
      expect(
        redirectFrom(beforeLoad, { params: { projectName: "demo-shop", tag } }),
      ).toBeNull();
    }
  });
});

describe("/projects/$projectName/tasks/$issueNumber — the task", () => {
  const params = taskRoute.options.params as {
    parse: (p: { issueNumber: string }) => { issueNumber: number };
    stringify: (p: { issueNumber: number }) => { issueNumber: string };
  };

  it("parses the segment to a number", () => {
    expect(params.parse({ issueNumber: "118" })).toMatchObject({ issueNumber: 118 });
  });

  it("rejects anything that is not a positive integer", () => {
    for (const bad of ["0", "-1", "1.5", "abc", ""]) {
      expect(() => params.parse({ issueNumber: bad })).toThrow(/invalid issue number/);
    }
  });

  it("round-trips back to a string for the URL", () => {
    expect(params.stringify({ issueNumber: 118 })).toMatchObject({
      issueNumber: "118",
    });
  });
});
