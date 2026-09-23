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

/**
 * The review bar's Preview/Annotate switch: a segmented pill whose raised thumb
 * slides to the active mode (instantly under reduced motion). Two `aria-pressed`
 * buttons in a "Mode" group; dimmed and inert while `disabled`.
 */

import type { ReactElement } from "react";
import { Box, ButtonBase, Typography } from "@wso2/oxygen-ui";
import { Eye, MessageSquarePlus } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeMode } from "../model/viewState";

export interface ModeToggleProps {
  mode: PrototypeMode;
  onChange: (mode: PrototypeMode) => void;
  disabled: boolean;
}

const MODES = [
  { value: "preview", label: "Preview", Icon: Eye },
  { value: "annotate", label: "Annotate", Icon: MessageSquarePlus },
] as const;

export function ModeToggle({ mode, onChange, disabled }: ModeToggleProps): ReactElement {
  const index = MODES.findIndex((m) => m.value === mode);
  return (
    <Box
      role="group"
      aria-label="Mode"
      sx={{
        position: "relative",
        display: "grid",
        gridTemplateColumns: "1fr 1fr",
        p: 0.5,
        borderRadius: 999,
        bgcolor: "action.hover",
        border: 1,
        borderColor: "divider",
        opacity: disabled ? 0.5 : 1,
      }}
    >
      <Box
        aria-hidden
        sx={(theme) => ({
          position: "absolute",
          top: theme.spacing(0.5),
          bottom: theme.spacing(0.5),
          left: theme.spacing(0.5),
          width: `calc((100% - ${theme.spacing(1)}) / 2)`,
          borderRadius: 999,
          bgcolor: "common.white",
          boxShadow: 1,
          ...theme.applyStyles("dark", { bgcolor: "grey.800" }),
          transform: `translateX(${index * 100}%)`,
          transition: theme.transitions.create("transform", { duration: theme.transitions.duration.short }),
          "@media (prefers-reduced-motion: reduce)": { transition: "none" },
        })}
      />
      {MODES.map(({ value, label, Icon }) => {
        const active = value === mode;
        return (
          <ButtonBase
            key={value}
            aria-pressed={active}
            disabled={disabled}
            onClick={() => {
              if (!active) onChange(value);
            }}
            sx={{
              position: "relative",
              zIndex: 1,
              gap: 0.75,
              px: 1.75,
              py: 0.625,
              borderRadius: 999,
              color: active ? "text.primary" : "text.secondary",
              fontWeight: active ? "fontWeightMedium" : "fontWeightRegular",
              "&.Mui-focusVisible": { outline: 2, outlineColor: "primary.main", outlineOffset: -2 },
            }}
          >
            <Icon size={15} aria-hidden />
            <Typography variant="body2" sx={{ fontWeight: "inherit" }}>
              {label}
            </Typography>
          </ButtonBase>
        );
      })}
    </Box>
  );
}
