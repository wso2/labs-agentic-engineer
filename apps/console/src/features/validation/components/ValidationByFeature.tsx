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

import { useState, type ReactNode } from "react";
import { Box, ButtonBase, Typography } from "@wso2/oxygen-ui";
import { StoryRef } from "../../design/components/viewers/StoryRef";
import { Tag } from "../../spec/components/Tag";
import type { FeatureResults, ScenarioResult } from "../../builds/model/validation";
import { LogTail, StepIcon } from "./RunParts";

// Validation, grouped by feature: a box per feature with its tally, a row per
// scenario tagged with the story it proves. A row opens for its evidence; a
// failing one says what it expected and what it got — and, against the
// previous validated version, whether it is a regression or still failing —
// and carries the build's next step (its fix) right there.
//
// The features come in three sections (B4): new in this version, already
// built (re-checked), and not built yet (designed, so their scenarios exist,
// but not run).

function groupTag(group: FeatureResults, settled: boolean): { tone: "success" | "warning" | "primary" | null; text: string } {
  const total = group.runs;
  if (!settled) return { tone: "primary", text: group.judged ? `${group.passed} of ${total} so far` : `${total} to run` };
  return group.passed === total
    ? { tone: "success", text: `${group.passed}/${total} passing` }
    : { tone: "warning", text: `${group.passed}/${total} passing` };
}

/** What a failure's standing says, against the version it is compared with. */
function standingText(scenario: ScenarioResult, baseline: string | null): string | null {
  if (!baseline) return null;
  if (scenario.standing === "regression") return `Regression: passed in ${baseline}`;
  if (scenario.standing === "still-failing") return `Still failing: it failed in ${baseline} too`;
  return null;
}

function ScenarioRow({
  projectName,
  scenario,
  baseline,
  actions,
}: {
  projectName: string;
  scenario: ScenarioResult;
  baseline: string | null;
  actions: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  if (scenario.outcome === "not-run") {
    return (
      <Box sx={{ borderTop: 1, borderColor: "divider", display: "flex", alignItems: "center", gap: 1.25, px: 1.5, py: 1 }}>
        <StepIcon state="queued" label="Not run" />
        <Typography variant="body2" color="text.secondary" sx={{ flex: 1, minWidth: 0 }}>
          {scenario.name} · not run: its story waits on a feature not built yet
        </Typography>
        {scenario.story && <StoryRef projectName={projectName} id={scenario.story} tag />}
      </Box>
    );
  }
  const judged = scenario.outcome !== "pending";
  const standing = standingText(scenario, baseline);
  const failed = judged && scenario.outcome !== "passed";
  return (
    <Box sx={{ borderTop: 1, borderColor: "divider" }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, pr: 1.5 }}>
        <ButtonBase
          disabled={!judged}
          aria-expanded={judged ? open : undefined}
          onClick={() => setOpen((v) => !v)}
          sx={{
            flex: 1,
            minWidth: 0,
            display: "flex",
            justifyContent: "flex-start",
            alignItems: "flex-start",
            gap: 1.25,
            px: 1.5,
            py: 1,
            textAlign: "start",
            "&:hover": judged ? { bgcolor: "action.hover" } : {},
          }}
        >
          <StepIcon
            state={!judged ? "queued" : failed ? "failed" : "done"}
            label={!judged ? "Not run yet" : failed ? "Failed" : "Passed"}
          />
          <Box component="span" sx={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
            <Typography component="span" variant="body2" color={judged ? "text.primary" : "text.secondary"}>
              {scenario.name}
            </Typography>
            {failed && scenario.expected && (
              <Typography component="span" variant="caption" color="error.main">
                Expected {scenario.expected}, got {scenario.got ?? "something else"}
              </Typography>
            )}
            {standing && (
              <Typography component="span" variant="caption" sx={{ fontWeight: 600, color: "warning.main" }}>
                {standing}
              </Typography>
            )}
          </Box>
        </ButtonBase>
        {scenario.story && <StoryRef projectName={projectName} id={scenario.story} tag />}
      </Box>
      {open && judged && (
        <Box sx={{ pl: 5.25, pr: 1.5, pb: 1.5, display: "flex", flexDirection: "column", gap: 0.75 }}>
          {failed ? (
            <>
              <Typography variant="body2">
                <b>Expected</b> {scenario.expected}
              </Typography>
              <Typography variant="body2">
                <b>Got</b> {scenario.got}
              </Typography>
            </>
          ) : (
            <Typography variant="caption" color="text.secondary">
              {scenario.story ? `Proves ${scenario.story}. ` : ""}
              {scenario.excerpt.filter((l) => !l.startsWith(" ")).length} steps passed.
            </Typography>
          )}
          <LogTail lines={scenario.excerpt} bordered />
          {failed && actions}
        </Box>
      )}
    </Box>
  );
}

/** The sections of a version's validation: new in it, built before it, not built yet. */
export function validationSections(
  groups: FeatureResults[],
  version: string,
  builtHere: readonly string[],
): { title: string; groups: FeatureResults[] }[] {
  const built = groups.filter((g) => !g.notBuilt);
  const sections = [
    { title: `New in ${version}`, groups: built.filter((g) => builtHere.includes(g.id)) },
    { title: "Already built", groups: built.filter((g) => !builtHere.includes(g.id)) },
    { title: "Not built yet", groups: groups.filter((g) => g.notBuilt) },
  ];
  return sections.filter((s) => s.groups.length > 0);
}

function SectionTitle({ children }: { children: ReactNode }) {
  return (
    <Typography
      component="h4"
      variant="caption"
      sx={{ letterSpacing: "0.06em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary", mt: 0.5 }}
    >
      {children}
    </Typography>
  );
}

export function ValidationByFeature({
  projectName,
  groups,
  version,
  builtHere,
  baseline,
  settled,
  failingActions,
}: {
  projectName: string;
  groups: FeatureResults[];
  /** The version validated, and the features it built itself (a repair builds none of its own). */
  version: string;
  builtHere: readonly string[];
  /** The previous validated version, which a failure's standing is against; null when none. */
  baseline: string | null;
  settled: boolean;
  /** The build's next steps, shown under a failing scenario's evidence. */
  failingActions: ReactNode;
}) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      {validationSections(groups, version, builtHere).map((section) => (
        <Box key={section.title} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
          <SectionTitle>{section.title}</SectionTitle>
          {section.title === "Not built yet" ? (
            <Typography variant="body2" color="text.secondary">
              {section.groups.map((g) => `${g.id} ${g.name}`).join(", ")} — designed, not run until built.
            </Typography>
          ) : (
            section.groups.map((group) => (
              <FeatureBox
                key={group.id}
                projectName={projectName}
                group={group}
                baseline={baseline}
                settled={settled}
                failingActions={failingActions}
              />
            ))
          )}
        </Box>
      ))}
    </Box>
  );
}

function FeatureBox({
  projectName,
  group,
  baseline,
  settled,
  failingActions,
}: {
  projectName: string;
  group: FeatureResults;
  baseline: string | null;
  settled: boolean;
  failingActions: ReactNode;
}) {
  const tag = groupTag(group, settled);
  return (
    <Box
      component="section"
      aria-label={`${group.name} validation`}
      sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, px: 1.5, py: 1.125, bgcolor: "background.default" }}>
        <Typography component="span" sx={{ fontFamily: "monospace", fontSize: "0.75rem", color: "text.secondary" }}>
          {group.id}
        </Typography>
        <Typography component="h4" variant="body2" sx={{ fontWeight: 600 }}>
          {group.name}
        </Typography>
        <Box sx={{ ml: "auto" }}>
          <Tag tone={tag.tone}>{tag.text}</Tag>
        </Box>
      </Box>
      {group.scenarios.map((s, i) => (
        <ScenarioRow key={`${s.name}-${i}`} projectName={projectName} scenario={s} baseline={baseline} actions={failingActions} />
      ))}
    </Box>
  );
}
