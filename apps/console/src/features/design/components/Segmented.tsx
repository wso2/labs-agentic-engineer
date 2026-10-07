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

import { ToggleButton, ToggleButtonGroup } from "@wso2/oxygen-ui";

/**
 * A small pill switch between a few values: the artifact filter, and an
 * artifact's Use it · Comment mode. Oxygen's ToggleButtonGroup, restyled as
 * the old console's wireframe view switch is (WireframePanel.tsx).
 */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
  fill = false,
}: {
  value: T;
  options: { value: T; label: string }[];
  onChange: (next: T) => void;
  label: string;
  /** Stretch across the width it is given, each option an equal share. */
  fill?: boolean;
}) {
  return (
    <ToggleButtonGroup
      size="small"
      exclusive
      value={value}
      aria-label={label}
      onChange={(_, next: T | null) => {
        if (next) onChange(next);
      }}
      sx={{
        bgcolor: "action.hover",
        borderRadius: 999,
        p: 0.25,
        ...(fill ? { display: "flex", width: "100%" } : {}),
        "& .MuiToggleButtonGroup-grouped": {
          border: 0,
          borderRadius: 999,
          px: 1.5,
          py: 0,
          height: 26,
          textTransform: "none",
          fontSize: "0.75rem",
          fontWeight: 500,
          color: "text.secondary",
          ...(fill ? { flex: 1 } : {}),
          "&.Mui-selected": {
            bgcolor: "background.paper",
            color: "text.primary",
            boxShadow: 1,
            "&:hover": { bgcolor: "background.paper" },
          },
        },
      }}
    >
      {options.map((o) => (
        <ToggleButton key={o.value} value={o.value}>
          {o.label}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  );
}
