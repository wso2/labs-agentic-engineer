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

import { useState } from "react";
import { Box, Button, ButtonBase, Typography } from "@wso2/oxygen-ui";
import type { FlowLane, FlowStep } from "../../api/designModel";
import { soft } from "../../../spec/components/Tag";
import { StoryRef } from "./StoryRef";

/** A lane's centre, as a percentage of the diagram's width. */
function lanePos(i: number, lanes: number): number {
  return ((i + 0.5) / lanes) * 100;
}

function stepWords(lanes: FlowLane[], step: FlowStep): string {
  const from = lanes[step.from]?.name ?? "";
  const to = lanes[step.to]?.name ?? "";
  return `${from}${step.from === step.to ? "" : ` → ${to}`}: ${step.text}`;
}

const ROW = 36;

/** The sequence: lanes across the top, one row per step, the current one in the accent colour. */
function Sequence({
  lanes,
  steps,
  current,
  onStep,
}: {
  lanes: FlowLane[];
  steps: FlowStep[];
  current: number;
  onStep: (k: number) => void;
}) {
  const n = lanes.length;
  return (
    <Box sx={{ flex: "2 1 420px", minWidth: 0, overflowX: "auto" }}>
      <Box sx={{ position: "relative", minWidth: 420, border: 1, borderColor: "divider", borderRadius: 2.5, pt: 1.25, pb: 1.75, bgcolor: "background.paper" }}>
        {lanes.map((lane, i) => (
          <Box
            key={lane.name}
            data-anchor={lane.name}
            sx={{ position: "absolute", top: 10, left: `${lanePos(i, n)}%`, transform: "translateX(-50%)", textAlign: "center", fontSize: "0.75rem", lineHeight: 1.2, whiteSpace: "nowrap" }}
          >
            <Box component="b" sx={{ display: "block", fontWeight: 600 }}>
              {lane.name}
            </Box>
            {lane.role && (
              <Box component="span" sx={{ color: "text.secondary", fontSize: "0.6875rem" }}>
                {lane.role}
              </Box>
            )}
          </Box>
        ))}
        <Box sx={{ position: "relative", mt: 5.5 }}>
          {lanes.map((lane, i) => (
            <Box key={lane.name} aria-hidden sx={{ position: "absolute", top: 0, bottom: 0, left: `${lanePos(i, n)}%`, borderLeft: "1px dashed", borderColor: "divider" }} />
          ))}
          {steps.map((step, k) => {
            const lo = Math.min(step.from, step.to);
            const hi = Math.max(step.from, step.to);
            const self = step.from === step.to;
            const on = k === current;
            const tone = on ? "primary.main" : k < current ? "divider" : "text.secondary";
            return (
              <ButtonBase
                key={k}
                data-anchor={`Step ${k + 1}`}
                aria-label={`Step ${k + 1}: ${stepWords(lanes, step)}`}
                aria-current={on ? "step" : undefined}
                onClick={() => onStep(k)}
                sx={{ position: "relative", display: "block", width: "100%", height: ROW, bgcolor: on ? soft("primary") : "transparent", "&:hover": { bgcolor: on ? soft("primary") : "action.hover" } }}
              >
                {self ? (
                  <Box sx={{ position: "absolute", top: 8, left: `${lanePos(lo, n)}%`, width: 26, height: 20, border: 2, borderLeft: 0, borderColor: tone, borderRadius: "0 8px 8px 0" }} />
                ) : (
                  <Box
                    sx={{
                      position: "absolute",
                      top: "50%",
                      height: 2,
                      left: `${lanePos(lo, n)}%`,
                      width: `${lanePos(hi, n) - lanePos(lo, n)}%`,
                      bgcolor: tone,
                      "&::after": {
                        content: '""',
                        position: "absolute",
                        top: -4,
                        borderTop: "5px solid transparent",
                        borderBottom: "5px solid transparent",
                        ...(step.to < step.from
                          ? { left: -1, borderRight: "7px solid", borderRightColor: tone }
                          : { right: -1, borderLeft: "7px solid", borderLeftColor: tone }),
                      },
                    }}
                  />
                )}
                <Box
                  component="span"
                  sx={{
                    position: "absolute",
                    top: 2,
                    left: `${self ? lanePos(lo, n) : (lanePos(lo, n) + lanePos(hi, n)) / 2}%`,
                    transform: "translateX(-50%)",
                    fontFamily: "monospace",
                    fontSize: "0.6875rem",
                    fontWeight: on ? 600 : 400,
                    color: on ? "primary.main" : "text.secondary",
                    bgcolor: "background.paper",
                    px: 0.5,
                    borderRadius: 1,
                  }}
                >
                  {k + 1}
                </Box>
              </ButtonBase>
            );
          })}
        </Box>
      </Box>
    </Box>
  );
}

/**
 * A flow, walked a step at a time: the step in words with the story it
 * comes from and its source, the steps as a list, and the sequence diagram.
 * Clicking a step in either moves to it.
 */
export function FlowView({ projectName, lanes, steps }: { projectName: string; lanes: FlowLane[]; steps: FlowStep[] }) {
  const [current, setCurrent] = useState(0);
  const at = Math.min(current, steps.length - 1);
  const step = steps[at];
  if (!step) return <Typography color="text.secondary">This flow has no steps yet.</Typography>;
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 3, alignItems: "flex-start" }}>
      <Box sx={{ flex: "1 1 260px", maxWidth: 420, display: "flex", flexDirection: "column", gap: 1 }}>
        <Typography sx={{ fontSize: "0.6875rem", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary" }}>
          Step {at + 1} of {steps.length}
        </Typography>
        <Typography data-anchor={`Step ${at + 1}`} sx={{ fontSize: "1.0625rem", fontWeight: 600, lineHeight: 1.35 }}>
          {stepWords(lanes, step)}
        </Typography>
        {(step.story || step.source) && (
          <Box sx={{ display: "flex", gap: 1, alignItems: "baseline", flexWrap: "wrap", fontSize: "0.8125rem", color: "text.secondary" }}>
            {step.story && <StoryRef projectName={projectName} id={step.story} />}
            {step.source && (
              <Box component="span" sx={{ fontFamily: "monospace", fontSize: "0.6875rem", border: 1, borderColor: "divider", borderRadius: 1, px: 0.625 }}>
                {step.source}
              </Box>
            )}
          </Box>
        )}
        <Box sx={{ display: "flex", gap: 1, mt: 0.5 }}>
          <Button size="small" variant="outlined" disabled={at === 0} onClick={() => setCurrent(at - 1)}>
            Previous step
          </Button>
          <Button size="small" variant="outlined" disabled={at === steps.length - 1} onClick={() => setCurrent(at + 1)}>
            Next step
          </Button>
        </Box>
        <Box component="ol" sx={{ m: 0, mt: 1, pl: 2.5, fontSize: "0.8125rem", color: "text.secondary", display: "flex", flexDirection: "column", gap: 0.375 }}>
          {steps.map((s, k) => (
            <Box component="li" key={k} data-anchor={`Step ${k + 1}`} sx={{ color: k === at ? "text.primary" : undefined, fontWeight: k === at ? 600 : 400 }}>
              <ButtonBase onClick={() => setCurrent(k)} sx={{ textAlign: "start", font: "inherit", color: "inherit" }}>
                {stepWords(lanes, s)}
              </ButtonBase>
            </Box>
          ))}
        </Box>
      </Box>
      <Sequence lanes={lanes} steps={steps} current={at} onStep={setCurrent} />
    </Box>
  );
}
