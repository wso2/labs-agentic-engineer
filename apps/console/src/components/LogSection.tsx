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
import {
  Box,
  Card,
  Collapse,
  IconButton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronDown, ChevronRight } from "@wso2/oxygen-ui-icons-react";

/**
 * The log surface every machine-written line in the console renders on.
 *
 * Lives here rather than beside one feature because three surfaces now render
 * logs — the coding agent's stream, a component's build log, and a running
 * workload's runtime log (ADR-0021) — and three dark boxes that drifted apart
 * would read as three different systems. `grey.900` is a theme token, so this
 * stays dark in BOTH schemes deliberately: a terminal that inverts in light mode
 * stops looking like a terminal.
 */
export function LogSurface({
  children,
  maxHeight = 420,
}: {
  children: ReactNode;
  maxHeight?: number;
}) {
  return (
    <Box
      sx={{
        bgcolor: "grey.900",
        borderRadius: 1,
        p: 2,
        maxHeight,
        overflowY: "auto",
        fontFamily: "monospace",
        fontSize: "0.8125rem",
        lineHeight: 1.7,
      }}
    >
      {children}
    </Box>
  );
}

/** A line of dimmed monospace on the log surface — empty states and notes. */
export function LogNote({ children }: { children: ReactNode }) {
  return (
    <Typography component="div" sx={{ font: "inherit", color: "grey.500" }}>
      {children}
    </Typography>
  );
}

/**
 * One timestamped line. The stamp is a fixed-width gutter so the messages line
 * up down the left edge instead of ragging behind stamps of different widths.
 */
export function LogLine({
  timestamp,
  children,
  tone = "default",
}: {
  timestamp?: string | undefined;
  children: ReactNode;
  tone?: "default" | "emphasis" | "warning" | "success";
}) {
  const color =
    tone === "emphasis"
      ? "common.white"
      : tone === "warning"
        ? "warning.light"
        : tone === "success"
          ? "info.light"
          : "grey.300";
  return (
    <Box sx={{ display: "flex", gap: 2, font: "inherit" }}>
      {timestamp && (
        <Box
          component="span"
          sx={{ color: "grey.500", flexShrink: 0, fontVariantNumeric: "tabular-nums" }}
        >
          {timestamp}
        </Box>
      )}
      <Box component="span" sx={{ color, minWidth: 0, wordBreak: "break-word" }}>
        {children}
      </Box>
    </Box>
  );
}

/**
 * A collapsible titled section — the build page's Tasks / Coding agent log /
 * Build logs, and the deployment page's per-component log.
 *
 * Open by default: the reader came here to read them, and a page of three
 * collapsed headers answers nothing. The disclosure is for getting a long log
 * out of the way, not for hiding the page's content on arrival.
 */
export function LogSection({
  title,
  meta,
  actions,
  children,
  defaultOpen = true,
  disablePadding = false,
}: {
  /** Plain text, deliberately NOT ReactNode: it is also the disclosure
   *  button's accessible name, and `String(<span/>)` is "[object Object]".
   *  Anything richer belongs in `meta`, which is not part of the label. */
  title: string;
  /** Counts, pills, or a live badge — sits beside the title. */
  meta?: ReactNode;
  /** Right-aligned controls: pickers, download, expand. */
  actions?: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
  /** Tables and lists bring their own padding; a log surface does not. */
  disablePadding?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <Card variant="outlined" sx={{ overflow: "hidden" }}>
      <Stack
        direction="row"
        spacing={1.25}
        sx={{
          alignItems: "center",
          px: 2.25,
          py: 1.5,
          borderBottom: open ? 1 : 0,
          borderColor: "divider",
        }}
      >
        <IconButton
          size="small"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          aria-label={open ? `Collapse ${title}` : `Expand ${title}`}
          sx={{ ml: -0.75 }}
        >
          {open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
        </IconButton>
        <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
          {title}
        </Typography>
        {meta}
        <Box sx={{ flex: 1 }} />
        {actions}
      </Stack>
      <Collapse in={open} unmountOnExit>
        <Box sx={disablePadding ? undefined : { p: 2 }}>{children}</Box>
      </Collapse>
    </Card>
  );
}
