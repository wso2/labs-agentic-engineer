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

// PROTOTYPE (throwaway). The floating variant switcher for UI prototypes:
// arrows cycle `?variant=`, ←/→ keys too (not while typing). Dev-only.

import { useEffect } from "react";
import { IconButton, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { ChevronLeft, ChevronRight } from "@wso2/oxygen-ui-icons-react";

export function PrototypeSwitcher({
  variants,
  current,
  names,
  onChange,
}: {
  variants: readonly string[];
  current: string;
  names: Record<string, string>;
  onChange: (next: string) => void;
}) {
  const idx = Math.max(0, variants.indexOf(current));
  const go = (delta: number) => onChange(variants[(idx + delta + variants.length) % variants.length]!);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
      if (e.key === "ArrowLeft") go(-1);
      if (e.key === "ArrowRight") go(1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  if (!import.meta.env.DEV) return null;
  return (
    <Paper
      elevation={12}
      sx={{
        position: "fixed",
        bottom: 12,
        left: "50%",
        transform: "translateX(-50%)",
        zIndex: (t) => t.zIndex.tooltip + 1,
        borderRadius: 8,
        px: 1,
        py: 0.5,
        bgcolor: "common.black",
        color: "common.white",
      }}
    >
      <Stack direction="row" alignItems="center" spacing={1}>
        <IconButton size="small" onClick={() => go(-1)} aria-label="Previous variant" sx={{ color: "inherit" }}>
          <ChevronLeft size={18} />
        </IconButton>
        <Typography variant="body2" sx={{ fontFamily: "monospace", minWidth: 260, textAlign: "center" }}>
          {current} · {names[current]}
        </Typography>
        <IconButton size="small" onClick={() => go(1)} aria-label="Next variant" sx={{ color: "inherit" }}>
          <ChevronRight size={18} />
        </IconButton>
      </Stack>
    </Paper>
  );
}
