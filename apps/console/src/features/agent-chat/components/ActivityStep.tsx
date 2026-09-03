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

import { Box, Chip, Collapse, Stack, Tooltip, Typography } from "@wso2/oxygen-ui";
import {
  Check,
  ChevronDown,
  ChevronRight,
  ListTodo,
  X as XIcon,
} from "@wso2/oxygen-ui-icons-react";
import type { ReactNode } from "react";
import type { ChatItem, ToolMessage } from "../toolGrouping";

// One tool step on a turn's activity rail (task 3). A single call is a plain
// row; a run of same-file edits (grouped by toolGrouping) collapses into a
// disclosure the user can expand. The status glyph doubles as the rail node.

function opLabel(op: string, status: "streaming" | "done"): string {
  const active = status === "streaming";
  switch (op) {
    case "add":
      return active ? "Creating" : "Created";
    case "remove":
      return active ? "Deleting" : "Deleted";
    default:
      return active ? "Editing" : "Edited";
  }
}

function leafName(path: string): string {
  return path.split("/").at(-1) ?? path;
}

// A small ring: spinning while the tool's body is still streaming, static once
// it has landed but the bundle has not ruled on it yet. Same ring either way, so
// the transition reads as "it stopped" rather than as a different thing.
function Ring({ spinning }: { spinning: boolean }) {
  return (
    <Box
      sx={{
        flexShrink: 0,
        width: 14,
        height: 14,
        borderRadius: "50%",
        border: "2px solid",
        borderColor: "divider",
        ...(spinning
          ? {
              borderTopColor: "primary.main",
              animation: "agentChatSpin 0.7s linear infinite",
              "@keyframes agentChatSpin": { to: { transform: "rotate(360deg)" } },
            }
          : {}),
      }}
    />
  );
}

/**
 * Three states, because two facts arrive on two frames (see `ChatMessage`): the
 * body is still streaming, the body has landed but the bundle has not ruled on
 * it, or the verdict is in. The middle one is brief — the agents service settles
 * each write at its own call — but it is not cosmetic: painting a success tick
 * there would claim a write the write-gates may still reject.
 */
function StatusGlyph({
  status,
  ok,
}: {
  status: "streaming" | "done";
  // `| undefined` is load-bearing under exactOptionalPropertyTypes: the caller
  // forwards `msg.ok`, which IS `boolean | undefined` on an unsettled card.
  ok?: boolean | undefined;
}) {
  if (status === "streaming") return <Ring spinning />;
  if (ok === undefined) return <Ring spinning={false} />;
  return ok ? (
    <Check size={14} color="var(--oxygen-palette-success-main, currentColor)" />
  ) : (
    <XIcon size={14} color="var(--oxygen-palette-error-main, currentColor)" />
  );
}

// Every step's first row is this tall (the small-chip height), with the status
// glyph centered against it — not against the whole step, so a wrapped error
// below never pulls the glyph off its line.
const STEP_ROW_HEIGHT = 24;

function GlyphCell({ children }: { children: ReactNode }) {
  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        flexShrink: 0,
        height: STEP_ROW_HEIGHT,
      }}
    >
      {children}
    </Box>
  );
}

/** The label + optional file chip for a single tool op, plus its full error.
 *  The error wraps (never ellipsizes) — a DSL validation message is the fix
 *  instruction, so cutting it to one line hides the actionable part. */
function StepLine({ msg, showFile }: { msg: ToolMessage; showFile: boolean }) {
  return (
    <Stack spacing={0.25} sx={{ minWidth: 0, flexGrow: 1 }}>
      <Stack
        direction="row"
        spacing={1}
        sx={{ alignItems: "center", minWidth: 0, minHeight: STEP_ROW_HEIGHT }}
      >
        <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
          {opLabel(msg.op, msg.status)}
        </Typography>
        {showFile && (
          <Tooltip title={msg.path}>
            <Chip size="small" variant="outlined" label={leafName(msg.path)} sx={{ maxWidth: 180 }} />
          </Tooltip>
        )}
      </Stack>
      {/* `=== false`, not `!msg.ok`: an unsettled card has no verdict yet, and
          "no verdict" must never read as "failed". */}
      {msg.ok === false && msg.errorText && (
        <Typography
          variant="caption"
          color="error"
          sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}
        >
          {msg.errorText}
        </Typography>
      )}
    </Stack>
  );
}

/**
 * A declare_plan row (#576, ADR-0025): the agent said what it is ABOUT to
 * write. One plain rail row — the plan's real rendering is the spec rail's
 * checklist; this row only keeps the chat's activity record complete, the way
 * every other tool call leaves a step.
 */
export function PlanStep({ added, grew }: { added: number; grew: boolean }) {
  return (
    <Stack
      data-testid="plan-step"
      direction="row"
      spacing={1}
      sx={{ alignItems: "flex-start", py: 0.25, minWidth: 0 }}
    >
      <GlyphCell>
        <ListTodo size={14} />
      </GlyphCell>
      <Stack
        direction="row"
        spacing={1}
        sx={{ alignItems: "center", minWidth: 0, minHeight: STEP_ROW_HEIGHT }}
      >
        <Typography variant="caption" color="text.secondary">
          {grew
            ? `Planned ${added} more ${added === 1 ? "document" : "documents"}`
            : `Planned ${added} ${added === 1 ? "document" : "documents"}`}
        </Typography>
      </Stack>
    </Stack>
  );
}

/**
 * Render one activity step. `expanded`/`onToggle` drive the disclosure for a
 * multi-op group; a lone op ignores them.
 */
export function ActivityStep({
  group,
  expanded,
  onToggle,
}: {
  group: Extract<ChatItem, { kind: "tool-group" }>;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { tools, path } = group;
  // A lone call: one plain row on the rail.
  if (tools.length <= 1) {
    const only = tools[0];
    if (!only) return null;
    return (
      <Stack
        data-testid="activity-step"
        direction="row"
        spacing={1}
        sx={{ alignItems: "flex-start", py: 0.25, minWidth: 0 }}
      >
        <GlyphCell>
          <StatusGlyph status={only.status} ok={only.ok} />
        </GlyphCell>
        <StepLine msg={only} showFile />
      </Stack>
    );
  }

  // A burst of edits to one file. Judge the group by where the file ENDED up
  // (the last op's state), so an intermediate failure a later op corrects
  // doesn't mark the whole run failed.
  const last = tools[tools.length - 1]!;
  return (
    <Box data-testid="activity-step" sx={{ minWidth: 0 }}>
      <Stack
        data-testid="activity-group"
        direction="row"
        spacing={1}
        role="button"
        aria-expanded={expanded}
        onClick={onToggle}
        sx={{
          alignItems: "center",
          py: 0.25,
          minWidth: 0,
          cursor: "pointer",
          borderRadius: 1,
          "&:hover": { bgcolor: "action.hover" },
        }}
      >
        <StatusGlyph status={last.status} ok={last.ok} />
        <Tooltip title={path}>
          <Chip size="small" variant="outlined" label={leafName(path)} sx={{ maxWidth: 180 }} />
        </Tooltip>
        <Typography variant="caption" color="text.secondary" sx={{ flexGrow: 1 }}>
          {tools.length} changes
        </Typography>
        {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
      </Stack>
      <Collapse in={expanded} unmountOnExit>
        <Stack spacing={0.25} sx={{ pl: 3, py: 0.25 }}>
          {tools.map((t) => (
            <Stack
              key={t.id}
              direction="row"
              spacing={1}
              sx={{ alignItems: "flex-start", minWidth: 0 }}
            >
              <GlyphCell>
                <StatusGlyph status={t.status} ok={t.ok} />
              </GlyphCell>
              <StepLine msg={t} showFile={false} />
            </Stack>
          ))}
        </Stack>
      </Collapse>
    </Box>
  );
}
