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

import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { featureScenarios, parseFeatureFile } from "./parseFeature.js";

const __DIRNAME = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(__DIRNAME, "../../../..");
const CHECKER = join(REPO, "skills/acceptance-run/scripts/check-report.mjs");

/**
 * The checker's own `scanFeature`, loaded from the shipped file rather than
 * copied — a copy would keep agreeing with itself after the original moved on.
 * The script runs work at import time, so the function is sliced out and
 * imported on its own.
 */
const checkerSource = readFileSync(CHECKER, "utf8");
const from = checkerSource.indexOf("function scanFeature(");
const to = checkerSource.indexOf("\nconst projectDir");
if (from < 0 || to < 0 || to < from) {
  throw new Error(`could not find scanFeature in ${CHECKER} — has the checker been restructured?`);
}
const { scanFeature } = (await import(
  `data:text/javascript,${encodeURIComponent(`${checkerSource.slice(from, to)}\nexport { scanFeature };`)}`
)) as { scanFeature: (text: string) => { feature: string; scenarios: { rule: string; name: string; line: number }[] } };

function featureFilesUnder(dir: string): string[] {
  let found: string[] = [];
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) found = found.concat(featureFilesUnder(p));
    else if (entry.endsWith(".feature")) found.push(p);
  }
  return found;
}

describe("agreement with the run's own checker", () => {
  // The report joins on identity, so a scenario the checker counts and this
  // parser does not would render as though the run had skipped it — and the
  // run's own contract gate would still be green. The two must not drift.
  //
  // Read from a COMMITTED corpus, not from a developer's playground: the files
  // have to be there for every reader of this repo, and the shapes worth
  // comparing on are the ones chosen for it — a comment, a docstring that
  // quotes Gherkin, the `Example:` synonym, an outline, and a file declaring no
  // feature at all — rather than whatever a local run happened to leave behind.
  const files = featureFilesUnder(join(__DIRNAME, "__fixtures__"));

  it("has a corpus to compare on", () => {
    expect(files.length).toBeGreaterThan(3);
  });

  it.each(files.map((f) => [f.slice(REPO.length + 1), f] as const))(
    "reads %s exactly as the checker does",
    (_label, file) => {
      const text = readFileSync(file, "utf8");
      const reference = scanFeature(text);
      const parsed = parseFeatureFile(file, text);

      if (reference.feature === "") {
        expect(parsed).toBeNull();
        return;
      }
      expect(parsed).not.toBeNull();
      const feature = parsed as NonNullable<typeof parsed>;
      expect(feature.name).toBe(reference.feature);
      expect(
        featureScenarios(feature).map(({ rule, scenario }) => ({
          rule: rule.text,
          name: scenario.name,
          line: scenario.line,
        })),
      ).toEqual(reference.scenarios);
    },
  );
});

describe("the shapes that fool a naive scanner", () => {
  it("does not read a Scenario: inside a docstring as a scenario", () => {
    const f = parseFeatureFile("a.feature", [
      "Feature: Quoting",
      "  Rule: A note is stored verbatim",
      "    Scenario: Storing a note",
      "      Given the note reads",
      '        """',
      "        Scenario: not a scenario",
      '        """',
      "      Then it is stored unchanged",
    ].join("\n"));
    expect(featureScenarios(f!).map((s) => s.scenario.name)).toEqual(["Storing a note"]);
  });

  it("ignores comments", () => {
    const f = parseFeatureFile("a.feature", [
      "# Scenario: not a scenario",
      "Feature: Comments",
      "  Rule: A rule",
      "    # Scenario: also not one",
      "    Scenario: The only one",
      "      Then it holds",
    ].join("\n"));
    expect(featureScenarios(f!).map((s) => s.scenario.name)).toEqual(["The only one"]);
  });

  it("treats Example: as the synonym for Scenario: that it is", () => {
    const f = parseFeatureFile("a.feature", [
      "Feature: Synonyms",
      "  Rule: A rule",
      "    Example: Written as an example",
      "      Then it holds",
    ].join("\n"));
    expect(featureScenarios(f!).map((s) => s.scenario.name)).toEqual(["Written as an example"]);
  });

  it("keeps the keyword each scenario was written with", () => {
    // The row prints it for anything that is not a plain `Scenario`, so an
    // outline reads as one rather than silently looking like a single case.
    const f = parseFeatureFile("a.feature", [
      "Feature: Keywords",
      "  Rule: A rule",
      "    Scenario: Written plainly",
      "      Then it holds",
      "    Scenario Outline: Written as an outline",
      "      Then it holds for <case>",
      "    Example: Written as an example",
      "      Then it holds",
    ].join("\n"));
    expect(featureScenarios(f!).map((s) => s.scenario.kind)).toEqual([
      "Scenario",
      "Scenario Outline",
      "Example",
    ]);
  });

  it("counts a Scenario Outline once and does not expand its Examples table", () => {
    const f = parseFeatureFile("a.feature", [
      "Feature: Outlines",
      "  Rule: A rule",
      "    Scenario Outline: Adding <item>",
      "      When Priya adds <item>",
      "      Then the list shows <item>",
      "      Examples:",
      "        | item  |",
      "        | Milk  |",
      "        | Bread |",
    ].join("\n"));
    const scenarios = featureScenarios(f!);
    expect(scenarios).toHaveLength(1);
    expect(scenarios[0]?.scenario.name).toBe("Adding <item>");
    expect(scenarios[0]?.scenario.steps.map((s) => s.keyword)).toEqual(["When", "Then"]);
  });

  it("returns null for text with no Feature: line, as the checker skips it", () => {
    expect(parseFeatureFile("a.feature", "Rule: orphaned\n  Scenario: nope")).toBeNull();
  });
});

describe("tags", () => {
  const FEATURE = [
    "@story-1",
    "Feature: Bought items",
    "",
    "  @story-6 @negative",
    "  Rule: A bought item is locked",
    "",
    "    Scenario: Editing a bought item is refused",
    "      Then the quantity is unchanged",
    "",
    "  @story-7",
    "  Rule: Bought items can be cleared",
    "",
    "    Scenario: Clearing them",
    "      Then they are gone",
    "",
    "    @negative",
    "    Scenario: Clearing an empty list changes nothing",
    "      Then nothing is removed",
  ].join("\n");

  it("inherits @negative down from the rule that carries it", () => {
    const f = parseFeatureFile("a.feature", FEATURE) as NonNullable<ReturnType<typeof parseFeatureFile>>;
    const byName = new Map(featureScenarios(f).map(({ scenario }) => [scenario.name, scenario]));
    expect(byName.get("Editing a bought item is refused")?.negative).toBe(true);
    expect(byName.get("Clearing them")?.negative).toBe(false);
    expect(byName.get("Clearing an empty list changes nothing")?.negative).toBe(true);
  });

  it("keeps a scenario's own tags separate from the ones it inherits", () => {
    const f = parseFeatureFile("a.feature", FEATURE) as NonNullable<ReturnType<typeof parseFeatureFile>>;
    const byName = new Map(featureScenarios(f).map(({ scenario }) => [scenario.name, scenario]));
    // The rule's @negative must not be copied onto the row, or every scenario
    // under a prohibition would print a tag it does not itself carry.
    expect(byName.get("Editing a bought item is refused")?.tags).toEqual([]);
    expect(byName.get("Clearing an empty list changes nothing")?.tags).toEqual(["@negative"]);
    expect(f.rules[0]?.tags).toEqual(["@story-6", "@negative"]);
    expect(f.tags).toEqual(["@story-1"]);
  });
});

describe("shapes the authoring skill discourages but Gherkin allows", () => {
  it("keeps Background steps off the scenario that follows them", () => {
    const f = parseFeatureFile("a.feature", [
      "Feature: Shared setup",
      "  Background:",
      "    Given the household has a shared list",
      "  Rule: A rule",
      "    Scenario: Adding an item",
      "      When Priya adds \"Milk\"",
      "      Then the list shows \"Milk\"",
    ].join("\n")) as NonNullable<ReturnType<typeof parseFeatureFile>>;
    expect(f.background.map((s) => s.text)).toEqual(["the household has a shared list"]);
    expect(featureScenarios(f)[0]?.scenario.steps.map((s) => s.keyword)).toEqual(["When", "Then"]);
  });

  it("files a scenario written straight under the Feature into an unnamed rule", () => {
    const f = parseFeatureFile("a.feature", [
      "Feature: No rules",
      "  Scenario: Standing alone",
      "    Then it holds",
    ].join("\n")) as NonNullable<ReturnType<typeof parseFeatureFile>>;
    expect(f.rules).toHaveLength(1);
    expect(f.rules[0]?.text).toBe("");
    expect(featureScenarios(f)).toHaveLength(1);
  });

  it("does not read a step's data table as steps", () => {
    const f = parseFeatureFile("a.feature", [
      "Feature: Tables",
      "  Rule: A rule",
      "    Scenario: With a table",
      "      Given the list holds",
      "        | name | qty |",
      "        | Milk | 2   |",
      "      Then it has 1 item",
    ].join("\n")) as NonNullable<ReturnType<typeof parseFeatureFile>>;
    expect(featureScenarios(f)[0]?.scenario.steps.map((s) => s.keyword)).toEqual(["Given", "Then"]);
  });
});
