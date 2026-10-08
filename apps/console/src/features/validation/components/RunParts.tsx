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

import { Box, keyframes } from "@wso2/oxygen-ui";
import { Check, X } from "@wso2/oxygen-ui-icons-react";
import type { PhaseState } from "../../builds/model/phases";

const pulse = keyframes`
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
`;

const LABEL: Record<PhaseState, string> = { done: "Done", live: "Running", queued: "Queued", failed: "Failed" };

/** A step's or a scenario's state as a small round mark, named for a screen reader: never colour alone. */
export function StepIcon({ state, label = LABEL[state] }: { state: PhaseState; label?: string }) {
  const filled = state === "done" || state === "failed";
  const colour = state === "done" ? "success.main" : state === "failed" ? "error.main" : state === "live" ? "primary.main" : "divider";
  return (
    <Box
      component="span"
      role="img"
      aria-label={label}
      sx={{
        width: 20,
        height: 20,
        flexShrink: 0,
        borderRadius: "50%",
        display: "grid",
        placeItems: "center",
        border: 1.5,
        borderColor: colour,
        bgcolor: filled ? colour : "transparent",
        color: "background.paper",
      }}
    >
      {state === "done" && <Check size={12} strokeWidth={3} aria-hidden />}
      {state === "failed" && <X size={12} strokeWidth={3} aria-hidden />}
      {state === "live" && (
        <Box
          component="span"
          sx={{
            width: 8,
            height: 8,
            borderRadius: "50%",
            bgcolor: "primary.main",
            animation: `${pulse} 1.2s ease-in-out infinite`,
            "@media (prefers-reduced-motion: reduce)": { animation: "none" },
          }}
        />
      )}
    </Box>
  );
}

/** The newest lines of a log, monospace, scrolled to its end. */
export function LogTail({ lines, empty = "Nothing yet.", bordered = false }: { lines: string[]; empty?: string; bordered?: boolean }) {
  const tail = lines.slice(-14);
  return (
    <Box
      component="pre"
      sx={{
        m: 0,
        px: 1.5,
        py: 1,
        bgcolor: "background.default",
        fontFamily: "monospace",
        fontSize: "0.72rem",
        lineHeight: 1.6,
        color: "text.secondary",
        whiteSpace: "pre-wrap",
        overflowWrap: "anywhere",
        ...(bordered ? { border: 1, borderColor: "divider", borderRadius: 2 } : { borderTop: 1, borderColor: "divider", borderTopStyle: "dashed" }),
      }}
    >
      {tail.length ? tail.join("\n") : empty}
    </Box>
  );
}
