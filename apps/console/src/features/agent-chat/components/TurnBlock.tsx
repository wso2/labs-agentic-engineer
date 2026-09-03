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

import { Fragment, type ReactNode } from "react";
import { alpha, Box, Link, Stack, Typography } from "@wso2/oxygen-ui";
import { Check, CircleQuestionMark, Sparkles, X as XIcon } from "@wso2/oxygen-ui-icons-react";
import { MarkdownView } from "../../../components/MarkdownView";
import type { ChatItem } from "../toolGrouping";
import type { FeedBlock } from "../feed";
import { ActivityStep, PlanStep } from "./ActivityStep";
import { WorkingIndicator } from "./WorkingIndicator";

// One agent turn in the activity stream (task 3): the "✦ Agent" header with
// teammate attribution, narration (markdown), tool steps on a vertical rail,
// and a lifecycle footer (running / committed / failed).

type TurnFeedBlock = Extract<FeedBlock, { kind: "turn" }>;

/** The activity rail: a vertical line the tool steps hang off. */
function ActivityRail({ children }: { children: ReactNode }) {
  return (
    <Box sx={{ borderLeft: 2, borderColor: "divider", ml: 1, pl: 2, my: 0.5 }}>
      <Stack spacing={0.25}>{children}</Stack>
    </Box>
  );
}

function TurnFooter({
  status,
  onOpenSpec,
  showSpecLink,
}: {
  status: TurnFeedBlock["status"];
  onOpenSpec: () => void;
  showSpecLink: boolean;
}) {
  if (status === "running") {
    return <WorkingIndicator label="Working…" />;
  }
  if (status === "failed") {
    return (
      <Stack
        data-testid="turn-failed"
        direction="row"
        spacing={0.75}
        sx={{ alignItems: "center", mt: 0.5, color: "error.main" }}
      >
        <XIcon size={14} />
        <Typography variant="caption" sx={{ fontWeight: 600 }}>
          Turn failed
        </Typography>
      </Stack>
    );
  }
  return (
    <Stack
      data-testid="turn-committed"
      direction="row"
      spacing={0.75}
      sx={{ alignItems: "center", mt: 0.5, color: "text.secondary" }}
    >
      <Check size={14} color="var(--oxygen-palette-success-main, currentColor)" />
      <Typography variant="caption">Turn committed</Typography>
      {showSpecLink ? (
        <>
          <Typography variant="caption" aria-hidden>
            ·
          </Typography>
          <Link
            component="button"
            type="button"
            variant="caption"
            onClick={onOpenSpec}
            sx={{ fontWeight: 600 }}
          >
            Open spec
          </Link>
        </>
      ) : null}
    </Stack>
  );
}

/** Questions live in the spec body's shared form — chat just points at them. */
function QuestionsPointer({ count, onOpen }: { count: number; onOpen: () => void }) {
  return (
    <Box
      data-testid="questions-pointer"
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      sx={{
        my: 0.5,
        px: 1.5,
        py: 1,
        borderRadius: 1.5,
        border: 1,
        borderColor: "primary.main",
        bgcolor: (theme) => alpha(theme.palette.primary.main, 0.08),
        cursor: "pointer",
        "&:hover": { bgcolor: (theme) => alpha(theme.palette.primary.main, 0.14) },
      }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
        <CircleQuestionMark size={16} />
        <Typography variant="body2" sx={{ fontWeight: 600, flexGrow: 1 }}>
          {count === 1 ? "The agent has a question" : `The agent has ${count} questions`}
        </Typography>
        <Typography variant="caption" color="primary" sx={{ fontWeight: 600 }}>
          {count === 1 ? "Answer it →" : "Answer them →"}
        </Typography>
      </Stack>
    </Box>
  );
}

/** Render the turn body items in order, batching consecutive tool steps onto
 *  one continuous rail while narration/errors stay flush-left. */
function TurnBody({
  items,
  expandedGroups,
  onToggleGroup,
  onOpenSpec,
  showSpecLink,
}: {
  items: ChatItem[];
  expandedGroups: Set<string>;
  onToggleGroup: (id: string) => void;
  onOpenSpec: () => void;
  showSpecLink: boolean;
}) {
  const out: ReactNode[] = [];
  let rail: ReactNode[] = [];
  const flushRail = (key: string) => {
    if (rail.length === 0) return;
    out.push(<ActivityRail key={key}>{rail}</ActivityRail>);
    rail = [];
  };

  items.forEach((item, idx) => {
    if (item.kind === "tool-group") {
      rail.push(
        <ActivityStep
          key={item.id}
          group={item}
          expanded={expandedGroups.has(item.id)}
          onToggle={() => onToggleGroup(item.id)}
        />,
      );
      return;
    }
    // A plan row is an activity step (#576): it joins the rail rather than
    // breaking it, so "Planned 4 more documents" sits between the file steps
    // it explains instead of splitting them into two rails.
    if (item.message.role === "plan") {
      rail.push(
        <PlanStep key={item.message.id} added={item.message.added} grew={item.message.grew} />,
      );
      return;
    }
    flushRail(`rail-${idx}`);
    const msg = item.message;
    if (msg.role === "error") {
      out.push(
        <Box
          key={msg.id}
          data-testid="chat-error"
          sx={{
            my: 0.5,
            px: 1.5,
            py: 1,
            borderRadius: 1,
            border: 1,
            borderColor: "error.main",
            color: "error.main",
            fontSize: "0.8125rem",
            whiteSpace: "pre-wrap",
          }}
        >
          {msg.content}
        </Box>,
      );
    } else if (msg.role === "assistant") {
      // Empty assistant messages appear briefly at a turn's start (created
      // before the first text delta); render nothing until they have content.
      if (msg.content) {
        out.push(<MarkdownView key={msg.id}>{msg.content}</MarkdownView>);
      }
    } else if (msg.role === "question" && msg.questions?.length && showSpecLink) {
      // EVERY question is answered on the spec body's shared form — one place,
      // one interaction, visible to the whole room. The chat only points at it
      // so the thread stays readable. Register chat has no spec room: questions
      // replace the form pane, so a pointer that navigates would be a lie.
      out.push(
        <QuestionsPointer key={msg.id} count={msg.questions.length} onOpen={onOpenSpec} />,
      );
    }
  });
  flushRail("rail-tail");
  return <Fragment>{out}</Fragment>;
}

export function TurnBlock({
  turn,
  expandedGroups,
  onToggleGroup,
  onOpenSpec,
  showSpecLink = true,
}: {
  turn: TurnFeedBlock;
  expandedGroups: Set<string>;
  onToggleGroup: (id: string) => void;
  onOpenSpec: () => void;
  showSpecLink?: boolean;
}) {
  return (
    <Box data-testid="turn-block">
      <Stack
        direction="row"
        spacing={0.75}
        sx={{ alignItems: "center", mb: 0.5, color: "text.secondary" }}
      >
        <Sparkles size={16} color="var(--oxygen-palette-primary-main, currentColor)" />
        <Typography variant="caption" sx={{ fontWeight: 600, color: "text.primary" }}>
          Agent
        </Typography>
        {/* Multi-user awareness: only surface attribution for a teammate's
            turn — your own turns don't need "· for You" noise. */}
        {!turn.attribution.isOwn && (
          <Typography variant="caption">· for {turn.attribution.displayName}</Typography>
        )}
      </Stack>
      <TurnBody
        items={turn.items}
        expandedGroups={expandedGroups}
        onToggleGroup={onToggleGroup}
        onOpenSpec={onOpenSpec}
        showSpecLink={showSpecLink}
      />
      <TurnFooter status={turn.status} onOpenSpec={onOpenSpec} showSpecLink={showSpecLink} />
    </Box>
  );
}
