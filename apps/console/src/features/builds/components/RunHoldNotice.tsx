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
import {
  Box,
  CircularProgress,
  Stack,
  Typography,
  alpha,
} from "@wso2/oxygen-ui";
import { CircleAlert, Info } from "@wso2/oxygen-ui-icons-react";
import type { StatusTone } from "../../../components/StatusChip";

// The run card's "why nothing is moving" panel.
//
// Deliberately NOT a MUI `Alert`. Alert's filled severity backgrounds are the
// loudest surface in the console, and this panel sits inside a card that
// already carries a state chip — two of them stacked (a run's hold, and the
// issue list's own gate banner underneath) turned an ordinary build into a page
// that looked like an incident. This is the same SOFT tint StatusChip uses for
// exactly that reason: the tone's own colour at low alpha, the tone's own text,
// and a rule down the leading edge so the panel reads as attached to the card
// rather than dropped on top of it.
//
// The palette family is read off the theme, never hardcoded, so the panel
// follows the active Oxygen theme like every other status surface.

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

export function RunHoldNotice({
  tone,
  title,
  body,
  /** A spinner in place of the icon: the platform is working, not blocked. */
  busy = false,
  /** The thing to do about it — a link to where the hold is released. */
  action,
  /** Detail the hold names, e.g. the gates it is held by. */
  children,
  /** `status` for a notice that appears on its own while the reader watches. */
  role,
}: {
  tone: Exclude<StatusTone, "neutral">;
  title: string;
  body: string;
  busy?: boolean;
  action?: ReactNode;
  children?: ReactNode;
  role?: "status";
}) {
  const family = TONE_PALETTE[tone];
  return (
    <Box
      {...(role ? { role } : {})}
      sx={(theme) => ({
        mt: 2,
        p: 1.75,
        borderRadius: 1,
        borderLeft: "3px solid",
        borderColor: theme.palette[family].main,
        bgcolor: alpha(theme.palette[family].main, 0.07),
      })}
    >
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ alignItems: "flex-start", flexWrap: "wrap", rowGap: 1 }}
      >
        <Box
          sx={(theme) => ({
            display: "flex",
            alignItems: "center",
            color: theme.palette[family].main,
            // Optical alignment with the title's cap height.
            mt: 0.25,
          })}
        >
          {busy ? (
            <CircularProgress size={16} color={family} />
          ) : tone === "info" ? (
            <Info size={18} />
          ) : (
            <CircleAlert size={18} />
          )}
        </Box>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography
            variant="subtitle2"
            sx={(theme) => ({ color: theme.palette[family].main })}
          >
            {title}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.25 }}>
            {body}
          </Typography>
          {children}
        </Box>
        {action}
      </Stack>
    </Box>
  );
}
