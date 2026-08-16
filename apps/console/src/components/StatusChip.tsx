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

import type { ReactNode } from "react";
import { Box, Chip, alpha } from "@wso2/oxygen-ui";

// The standard clip-rect recipe: out of view, still in the accessibility tree.
// Inlined rather than imported so this component keeps its one Oxygen dependency.
const VISUALLY_HIDDEN = {
  position: "absolute",
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
  border: 0,
} as const;

// Console-wide status/kind chip (Task 4): every domain (task status, alert
// classification, skill origin, project phase, ...) maps its own vocabulary
// to one of these tones in a single place — see each feature's own
// `*chip`/`*Tone` mapping function — so every pill in the app shares one
// size, shape, and palette instead of three divergent chip components.
// `primary` covers a "brand" kind (the org skill origin) that isn't a state
// but still needs to stand out from the neutral default.
export type StatusTone =
  | "success"
  | "info"
  | "warning"
  | "error"
  | "neutral"
  | "primary";

const TONE_COLOR: Record<
  StatusTone,
  "default" | "primary" | "success" | "info" | "warning" | "error"
> = {
  success: "success",
  info: "info",
  warning: "warning",
  error: "error",
  neutral: "default",
  primary: "primary",
};

// The palette family each tone reads its soft tint from. `neutral` has no
// palette family, so its soft look is derived from text/action tokens below.
const TONE_PALETTE: Record<
  Exclude<StatusTone, "neutral">,
  "primary" | "success" | "info" | "warning" | "error"
> = {
  success: "success",
  info: "info",
  warning: "warning",
  error: "error",
  primary: "primary",
};

// `soft`: a low-emphasis status pill — a faint tinted background with the
// tone's own text colour and no border — for spots where a solid filled chip
// reads as a button (a status beside a page title, a build's live state).
// `dot` prefixes a small tone-coloured dot so the pill reads as a live status
// indicator ("● Running"). Solid stays the default so dense rows are
// unaffected.
export function StatusChip({
  label,
  spokenLabel,
  tone,
  variant,
  appearance = "solid",
  dot = false,
}: {
  label: string;
  /**
   * The accessible name, for a label that carries meaning in PUNCTUATION a screen
   * reader does not announce — "Validated*" would otherwise be heard as "Validated",
   * indistinguishable from a clean pass. Omitted everywhere else, which keeps the
   * default "the name is the visible label".
   */
  spokenLabel?: string;
  tone: StatusTone;
  variant?: "filled" | "outlined";
  appearance?: "solid" | "soft";
  dot?: boolean;
}) {
  // Visually-hidden TEXT, not aria-label on the root. A Chip with no onClick renders
  // a plain div with no role, and an aria-label on a roleless element is ignored by
  // screen readers — so the accessible name has to come from content. The visible
  // string is hidden from the a11y tree and the spoken one takes its place.
  const named = (visible: ReactNode): ReactNode =>
    spokenLabel ? (
      <>
        <span aria-hidden>{visible}</span>
        <Box component="span" sx={VISUALLY_HIDDEN}>
          {spokenLabel}
        </Box>
      </>
    ) : (
      visible
    );
  if (appearance === "soft") {
    const isNeutral = tone === "neutral";
    const labelNode = dot ? (
      <Box sx={{ display: "inline-flex", alignItems: "center", gap: 0.75 }}>
        <Box
          sx={(theme) => ({
            width: 6,
            height: 6,
            borderRadius: "50%",
            bgcolor: isNeutral
              ? theme.palette.text.secondary
              : theme.palette[TONE_PALETTE[tone]].main,
          })}
        />
        <span>{label}</span>
      </Box>
    ) : (
      label
    );
    return (
      <Chip
        size="small"
        label={named(labelNode)}
        sx={(theme) =>
          isNeutral
            ? {
                bgcolor: theme.palette.action.hover,
                color: "text.secondary",
                fontWeight: 500,
              }
            : {
                bgcolor: alpha(theme.palette[TONE_PALETTE[tone]].main, 0.14),
                color: theme.palette[TONE_PALETTE[tone]].main,
                fontWeight: 500,
              }
        }
      />
    );
  }

  return (
    <Chip
      size="small"
      label={named(label)}
      color={TONE_COLOR[tone]}
      {...(variant ? { variant } : {})}
    />
  );
}
