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

import { describe, expect, it } from "vitest";
import type { components } from "../../../generated/aep-api";
import {
  asConnectionRow,
  declaredExternalCount,
  displayState,
  externalResourceHeadline,
  externalResourceRows,
} from "./externalResourceRows";

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type Dependency = components["schemas"]["Dependency"];
type ProjectDependencyReadiness =
  components["schemas"]["ProjectDependencyReadiness"];

function external(
  name: string,
  keys: string[],
  description?: string,
): Dependency {
  return {
    kind: "external",
    name,
    ...(description && { description }),
    config: keys.map((key) => ({ key })),
  };
}

function design(...deps: Dependency[][]): ComponentDependencies[] {
  return deps.map((dependencies, i) => ({
    componentName: `component-${i + 1}`,
    dependencies,
  }));
}

function readiness(
  ...deps: { name: string; state: string; missingKeys?: string[] }[]
): ProjectDependencyReadiness {
  return {
    configured: deps.every((d) => d.state === "configured"),
    dependencies: deps.map((d) => ({
      name: d.name,
      state: d.state as ProjectDependencyReadiness["dependencies"][number]["state"],
      missingKeys: d.missingKeys ?? [],
    })),
  };
}

describe("displayState", () => {
  it("counts a configured dependency as done", () => {
    expect(displayState("configured")).toBe("configured");
  });

  it("counts an unset dependency as something to do", () => {
    expect(displayState("unset")).toBe("needs-values");
  });

  // THE rule this module exists to get right. Nothing provisions an external
  // dependency in the background — the only thing that fills one is a person
  // typing values, and SaveValues authors the resource from scratch. So
  // `not-provisioned` is an ask, not a wait. Reading it as "the platform is
  // provisioning it" would hide the Configure button on every dependency no
  // build has authored yet, with nothing ever coming to move it off that state.
  it("treats not-provisioned as needing a person, never as platform work", () => {
    expect(displayState("not-provisioned")).toBe("needs-values");
  });
});

describe("externalResourceRows", () => {
  it("joins the design's schema with what the platform holds", () => {
    const rows = externalResourceRows(
      design([external("stripe", ["api_key", "webhook_secret"], "Payments")]),
      readiness({
        name: "stripe",
        state: "unset",
        missingKeys: ["webhook_secret"],
      }),
    );
    expect(rows).toEqual([
      {
        name: "stripe",
        description: "Payments",
        config: [{ key: "api_key" }, { key: "webhook_secret" }],
        state: "unset",
        display: "needs-values",
        missingCount: 1,
      },
    ]);
  });

  // A platform resource's credentials are the platform's own; the run's
  // provisioning gates already report it, and there is nothing to type in.
  it("lists externals only — a platform resource has no values to supply", () => {
    const rows = externalResourceRows(
      design([
        external("stripe", ["api_key"]),
        {
          kind: "platform-resource",
          name: "orders-db",
          resourceType: "postgres-cnpg",
          config: [{ key: "url" }],
        },
        { kind: "component", name: "catalog-api", config: [] },
      ]),
      readiness({ name: "stripe", state: "configured" }),
    );
    expect(rows.map((r) => r.name)).toEqual(["stripe"]);
  });

  it("asks once for a dependency two components share", () => {
    const rows = externalResourceRows(
      design(
        [external("stripe", ["api_key"])],
        [external("stripe", ["api_key"]), external("sendgrid", ["token"])],
      ),
      readiness(
        { name: "stripe", state: "configured" },
        { name: "sendgrid", state: "unset", missingKeys: ["token"] },
      ),
    );
    expect(rows.map((r) => r.name)).toEqual(["sendgrid", "stripe"]);
  });

  // Two components declaring the same dependency with DISJOINT keys. Readiness
  // is computed against the union, so a dialog offering only the first
  // component's keys could never satisfy it — the row would stay "Needs configuration"
  // however many times it was saved, and the deploy gate would never open.
  it("unions the config keys of a dependency two components declare differently", () => {
    const rows = externalResourceRows(
      design(
        [external("stripe", ["api_key"])],
        [external("stripe", ["webhook_secret"])],
      ),
      readiness({
        name: "stripe",
        state: "unset",
        missingKeys: ["api_key", "webhook_secret"],
      }),
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]!.config.map((c) => c.key)).toEqual([
      "api_key",
      "webhook_secret",
    ]);
  });

  // Secret wins on conflict, mirroring spec.UnionExternalConfigKeys. A key any
  // component marks secret must never be collected as a plain value.
  it("keeps a key secret when any component declares it secret", () => {
    const plain: Dependency = {
      kind: "external",
      name: "stripe",
      config: [{ key: "api_key" }],
    };
    const secret: Dependency = {
      kind: "external",
      name: "stripe",
      config: [{ key: "api_key", secret: true }],
    };
    const rows = externalResourceRows(
      design([plain], [secret]),
      readiness({ name: "stripe", state: "unset", missingKeys: ["api_key"] }),
    );
    expect(rows[0]!.config).toEqual([{ key: "api_key", secret: true }]);
  });

  // A dependency with an empty schema has nothing to collect, so a row for it
  // would open a dialog with no fields — even when readiness names it.
  it("skips an external that declares no config keys", () => {
    expect(
      externalResourceRows(
        design([external("pinger", [])]),
        readiness({ name: "pinger", state: "unset" }),
      ),
    ).toEqual([]);
  });

  // A build has not authored the binding yet, but readiness derives from the
  // design, so it still names the dependency — as `not-provisioned`, which is
  // an ask, not a wait.
  it("renders a dependency readiness reports as never provisioned", () => {
    const rows = externalResourceRows(
      design([external("stripe", ["api_key", "webhook_secret"])]),
      readiness({
        name: "stripe",
        state: "not-provisioned",
        missingKeys: ["api_key", "webhook_secret"],
      }),
    );
    expect(rows[0]).toMatchObject({
      state: "not-provisioned",
      display: "needs-values",
      missingCount: 2,
    });
  });

  // FINDING 6 — a Registered External holds its values on the ORG catalog
  // record. `provisioning.SaveValues` answers 409 `values live on the org
  // record` for one, and the deploy gate deliberately excludes it (naming it
  // would park a run forever on something no project surface can clear), so
  // readiness omits it. Defaulting an unmentioned dependency to
  // `not-provisioned` would resurrect exactly that row: a "Needs configuration" chip
  // and a Configure button that 409s, under a headline contradicting a gate
  // that is not blocked.
  it("does not render an external the readiness read omits", () => {
    const rows = externalResourceRows(
      design([
        external("stripe", ["api_key"]),
        external("acme-registered", ["ACME_TOKEN"]),
      ]),
      readiness({ name: "stripe", state: "unset", missingKeys: ["api_key"] }),
    );
    expect(rows.map((r) => r.name)).toEqual(["stripe"]);
  });

  // The same rule at its limit: readiness that names nothing renders nothing,
  // rather than every declared dependency as outstanding.
  it("renders no rows when a successful readiness read names none", () => {
    expect(
      externalResourceRows(design([external("stripe", ["api_key"])]), readiness()),
    ).toEqual([]);
  });

  // Readiness enumerates from the design, so this is the design read lagging
  // the platform — not a row to invent a config schema for.
  it("ignores a readiness entry the design does not describe", () => {
    expect(
      externalResourceRows(
        design([external("stripe", ["api_key"])]),
        readiness(
          { name: "stripe", state: "configured" },
          { name: "removed-last-week", state: "unset" },
        ),
      ).map((r) => r.name),
    ).toEqual(["stripe"]);
  });

  // FINDING 7/8 — an ABSENT readiness response is "not known yet", never "no
  // externals". The section, not this function, decides whether that is a
  // pending read or a failed one.
  it("renders nothing without a readiness response, even with a design", () => {
    expect(
      externalResourceRows(design([external("stripe", ["api_key"])]), undefined),
    ).toEqual([]);
  });

  it("matches the platform's slugged name against the design's spelling", () => {
    const rows = externalResourceRows(
      design([external("Stripe", ["api_key"])]),
      readiness({ name: "stripe", state: "configured" }),
    );
    // The DESIGN's spelling is what a person reads; the state is the
    // platform's.
    expect(rows[0]).toMatchObject({ name: "Stripe", display: "configured" });
  });

  it("renders nothing before either read has arrived", () => {
    expect(externalResourceRows(undefined, undefined)).toEqual([]);
  });
});

describe("externalResourceHeadline", () => {
  it("says how many still want values", () => {
    const rows = externalResourceRows(
      design([
        external("stripe", ["api_key"]),
        external("sendgrid", ["token"]),
        external("twilio", ["sid"]),
        external("segment", ["key"]),
      ]),
      readiness(
        { name: "stripe", state: "configured" },
        { name: "sendgrid", state: "configured" },
        { name: "twilio", state: "unset", missingKeys: ["sid"] },
        { name: "segment", state: "not-provisioned", missingKeys: ["key"] },
      ),
    );
    // twilio is unset and segment has no binding yet — both outstanding.
    expect(externalResourceHeadline(rows)).toBe("2 of 4 need configuration");
  });

  // The headline counts the SUPPLIABLE set, so an external readiness omits is
  // not in the denominator either — "1 of 2" would contradict a deploy gate
  // that only waits on one.
  it("counts only what the readiness read named", () => {
    const rows = externalResourceRows(
      design([
        external("stripe", ["api_key"]),
        external("acme-registered", ["ACME_TOKEN"]),
      ]),
      readiness({ name: "stripe", state: "unset", missingKeys: ["api_key"] }),
    );
    expect(externalResourceHeadline(rows)).toBe("1 of 1 need configuration");
  });

  it("says so once every one is configured", () => {
    const rows = externalResourceRows(
      design([external("stripe", ["api_key"])]),
      readiness({ name: "stripe", state: "configured" }),
    );
    expect(externalResourceHeadline(rows)).toBe("1 of 1 configured");
  });
});

// The one thing the DESIGN read alone supports, and what the section falls
// back to when readiness is unknown (findings 7 and 8): how many externals
// somebody could be asked to supply — never how many still need configuration.
describe("declaredExternalCount", () => {
  it("counts collectable externals once each, whatever readiness says", () => {
    expect(
      declaredExternalCount(
        design(
          [external("stripe", ["api_key"])],
          [
            external("stripe", ["api_key"]),
            external("sendgrid", ["token"]),
            external("pinger", []),
            { kind: "component", name: "catalog-api" },
            {
              kind: "platform-resource",
              name: "orders-db",
              resourceType: "postgres-cnpg",
              config: [{ key: "url" }],
            },
          ],
        ),
      ),
    ).toBe(2);
  });

  it("is 0 without a design", () => {
    expect(declaredExternalCount(undefined)).toBe(0);
  });
});

describe("asConnectionRow", () => {
  it("hands the shared dialog the schema it collects against", () => {
    const [row] = externalResourceRows(
      design([external("stripe", ["api_key"], "Payments")]),
      readiness({ name: "stripe", state: "unset", missingKeys: ["api_key"] }),
    );
    expect(asConnectionRow(row!)).toEqual({
      id: "stripe",
      name: "stripe",
      kind: "external",
      config: [{ key: "api_key" }],
      provisioned: false,
    });
  });
});
