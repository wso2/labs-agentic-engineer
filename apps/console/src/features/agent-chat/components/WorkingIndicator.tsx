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

import { Box, Stack, Typography } from "@wso2/oxygen-ui";

// A subtle "the agent is working" indicator (task 3): a soft pulsing dot plus
// a label. Replaces the old ThinkingDots — used as a running turn's footer and
// as the tail placeholder before a turn has produced any content.
/**
 * The dot alone — "an agent is working", with nothing said about what.
 *
 * Extracted so the spec rail's active section pulses with the SAME animation
 * rather than growing its own (#575): work in progress should look identical
 * everywhere it appears, and a second easing curve on a second surface is how
 * that stops being true.
 */
export function WorkingPulse() {
  return (
    <Box
      data-testid="working-pulse"
      sx={{
        width: 8,
        height: 8,
        flexShrink: 0,
        borderRadius: "50%",
        bgcolor: "primary.main",
        animation: "agentChatWorkingPulse 1.2s ease-in-out infinite",
        "@keyframes agentChatWorkingPulse": {
          "0%, 100%": { opacity: 0.3, transform: "scale(0.85)" },
          "50%": { opacity: 1, transform: "scale(1)" },
        },
        // A reader who has asked their OS for less motion still needs to know
        // an agent is working, so the dot stays — at the animation's bright
        // end, rather than at whatever opacity it happened to stop on. The
        // overview track brought this guard with it (#575 unified the three
        // surfaces on this component; the ring it replaced honoured the
        // preference and this did not), and it belongs here, where all three
        // get it.
        "@media (prefers-reduced-motion: reduce)": {
          animation: "none",
          opacity: 1,
        },
      }}
    />
  );
}

export function WorkingIndicator({ label = "Working…" }: { label?: string }) {
  return (
    <Stack
      data-testid="working"
      direction="row"
      spacing={1}
      sx={{ alignItems: "center", mt: 0.5, color: "text.secondary" }}
    >
      <WorkingPulse />
      <Typography variant="caption">{label}</Typography>
    </Stack>
  );
}
