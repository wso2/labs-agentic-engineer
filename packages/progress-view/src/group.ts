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

// Who produced an event, and how a fan-out is arranged for reading.
//
// The v1 grouping INFERRED the tree: a line was stamped `main` or `subagent`,
// and a subagent's identity was the id of the tool call that spawned it. That
// carried exactly one level and nothing else — a subagent that fanned out again
// had its work filed under itself, and a reader who joined the stream late could
// not tell which agent was whose.
//
// v2 DECLARES it. `agent_started` names the agent, its parent (`parentAgentId`)
// and how far from the lead it sits (`depth`), so the tree is read off the wire
// instead of being guessed at — including by a consumer that never saw the
// parents, which is what `depth` is recorded rather than derived for.

import {
  agentReportFromStarted,
  settleAgentReport,
  formatHeartbeat,
  isSilentKind,
  type RunEventView,
} from "./event.js";
import { LEAD_AGENT_ID, openAgentReport, type AgentReport } from "./agent.js";

/** A row in an agent's section: one of its own events, or a whole child agent. */
export type EventRow<E> =
  | { kind: "event"; event: E }
  | { kind: "section"; section: AgentSection<E> };

/**
 * One agent's whole section of the feed.
 *
 * `rows` holds the agent's own events with every agent IT spawned nested at the
 * point that child first spoke — so a fan-out still reads in the order it
 * happened while each agent's work stays contiguous. Read flat, three
 * components' work reads as one agent contradicting itself.
 *
 * `agent` is the header: what the agent is doing now while it runs, and the
 * runtime's authoritative totals plus its closing REPORT once it settles. That
 * is what a collapsed section shows, so choosing not to expand one still tells
 * you whether it worked and what it says it did.
 */
export interface AgentSection<E> {
  id: string;
  /** The agent that spawned it — absent on the lead. */
  parentId?: string | undefined;
  /** Distance from the lead: 0 is the lead itself, 1 an agent it spawned. */
  depth: number;
  agent: AgentReport;
  rows: EventRow<E>[];
}

/**
 * Arrange one cycle's events into the declared agent tree, rooted at the lead.
 *
 * Silent kinds never become rows: `agent_progress` and `heartbeat` repaint the
 * owning agent's header, `work_item` belongs to whatever surface holds the item
 * rows, and an agent's own `agent_started` / `agent_settled` ARE the section's
 * header and footer rather than lines inside it.
 */
export function groupByAgent<E extends RunEventView>(events: readonly E[]): AgentSection<E> {
  const lead: AgentSection<E> = {
    id: LEAD_AGENT_ID,
    depth: 0,
    agent: openAgentReport(LEAD_AGENT_ID, "lead agent"),
    rows: [],
  };
  const sections = new Map<string, AgentSection<E>>([[LEAD_AGENT_ID, lead]]);
  // Agents whose section is mid-construction, so a parent chain that loops back
  // on itself terminates. See the fallback in `sectionFor`.
  const opening = new Set<string>();

  /**
   * The section for an agent, opening it — and any ancestor still unseen — where
   * this is the first anyone has heard of it.
   *
   * An agent with no `agent_started` is not dropped and not filed under a guess:
   * it gets a section named by its own id, hung off the parent it claims, or off
   * the lead when it claims none. That is the shape of a feed joined mid-run,
   * and of one the platform admits has gaps.
   */
  const sectionFor = (
    agentId: string,
    parentId?: string | undefined,
    depth?: number | undefined,
  ): AgentSection<E> => {
    // Ancestry is settled once, when the section is drawn. A section already
    // placed under the lead cannot be moved when a late `agent_started` names a
    // different parent: it is already on screen there, and re-parenting mid-feed
    // would make the tree jump under a reader. The normal path never hits this —
    // an `agent_started` precedes everything its agent does.
    const existing = sections.get(agentId);
    if (existing) return existing;
    // An agent that claims itself — or an ancestor still being opened — as its
    // parent would recurse forever here, and then render a section inside
    // itself. It hangs off the lead instead: a mis-parented agent still appears,
    // at the top level, rather than wedging the surface that shows it.
    const claimed =
      parentId && parentId !== agentId && !opening.has(parentId) ? parentId : LEAD_AGENT_ID;
    opening.add(agentId);
    const parent = agentId === LEAD_AGENT_ID ? undefined : sectionFor(claimed);
    opening.delete(agentId);
    const section: AgentSection<E> = {
      id: agentId,
      parentId: parent?.id,
      depth: depth ?? (parent ? parent.depth + 1 : 0),
      agent: openAgentReport(agentId),
      rows: [],
    };
    sections.set(agentId, section);
    // Placed where the agent FIRST spoke, which is what keeps the fan-out in the
    // order it happened.
    parent?.rows.push({ kind: "section", section });
    return section;
  };

  for (const event of events) {
    const section = sectionFor(event.agentId, event.parentAgentId, event.depth);
    switch (event.kind) {
      case "agent_started": {
        // The header, not a row in the section it opens. Spread OVER whatever
        // was already learned rather than replacing it: a settle or a phrase can
        // precede the start on a feed joined mid-run or replayed out of order,
        // and an agent that has already finished must not revert to `running`.
        const opened = agentReportFromStarted(event);
        section.agent = {
          ...section.agent,
          label: opened.label,
          role: opened.role,
          background: opened.background,
          // The arrival of this event IS the declaration — see
          // AgentReport.declared. Copied field by field like the rest, because
          // the counters already folded onto this section outrank a start that
          // knows nothing about them.
          declared: opened.declared,
        };
        continue;
      }
      case "agent_settled":
        section.agent = settleAgentReport(section.agent, event);
        continue;
      case "agent_progress":
        // A phrase REPLACES the previous one: it is a state, not a log line.
        section.agent.activity = event.phrase;
        continue;
      case "heartbeat":
        // The silence explained, in the one place it belongs — the agent's live
        // status. A heartbeat only fires when nothing else is happening, so it
        // is fresher than whatever phrase it displaces, and the next phrase
        // displaces it right back.
        section.agent.activity = formatHeartbeat(event);
        continue;
      default:
        if (isSilentKind(event.kind)) continue;
        section.rows.push({ kind: "event", event });
    }
  }
  return lead;
}

/** The fields pairing an action with its outcome, in either envelope version. */
export interface OutcomePairable {
  kind: string;
  toolUseId?: string | undefined;
}

/**
 * One action and its outcome, for a surface that can hold both before drawing.
 *
 * The wire carries them as two events on purpose: an action is emitted the
 * instant the runtime yields it, BEFORE the command runs, which is what makes
 * the feed live. A cold `bal build` does not report its exit code for another 25
 * seconds. Delaying the action row until then would buy aligned rows at the
 * price of 25 seconds of silence per build, so merging is a renderer's job
 * rather than the wire's.
 *
 * A tool_result whose action was never seen (an evicted pending entry, a feed
 * joined mid-run) stays a row of its own — dropping it would hide a failure.
 */
export interface MergedRow<E> {
  line: E;
  outcome?: E | undefined;
  /**
   * The action put its command in the BACKGROUND and has not been answered yet.
   *
   * Known from the `task_started` sharing this action's `toolUseId`, which is a
   * silent kind and draws no row of its own. Without this a backgrounded build
   * is indistinguishable from a call still in flight, and a reader watching a
   * row with no outcome for four minutes has no way to tell "detached, come back
   * later" from "waiting on this right now".
   */
  backgrounded?: boolean | undefined;
}

/** The kinds that ANSWER an action rather than being one. */
const IS_OUTCOME = new Set(["tool_result", "task_settled"]);

export function mergeOutcomes<E extends OutcomePairable>(events: readonly E[]): MergedRow<E>[] {
  const rows: MergedRow<E>[] = [];
  const byToolUse = new Map<string, MergedRow<E>>();
  for (const line of events) {
    // A `task_settled` is an outcome in exactly the same sense as a
    // `tool_result`: the row for the command was drawn the instant the runtime
    // yielded it, and this says how it ended, minutes later. Folding it is what
    // stops a backgrounded command being drawn twice — once as the action, once
    // as a settle repeating the same command line, which is how one `gh issue
    // comment` filled four lines of a live feed.
    if (IS_OUTCOME.has(line.kind) && line.toolUseId) {
      const action = byToolUse.get(line.toolUseId);
      if (action) {
        action.outcome = line;
        continue;
      }
    }

    // The start of a backgrounded command marks the action it belongs to rather
    // than becoming a row. It carries the same `toolUseId`, so it must NOT claim
    // the action's place in the map either — the settle has to land on the row a
    // reader can actually see.
    if (line.kind === "task_started" && line.toolUseId) {
      const action = byToolUse.get(line.toolUseId);
      if (action) {
        action.backgrounded = true;
        continue;
      }
    }
    const row: MergedRow<E> = { line };
    rows.push(row);
    // Only an ACTION claims the id: a tool_result kept as its own row must not
    // then swallow a later result, and the git_*/gh_action rewrites of a Bash
    // call are actions too, so they take their outcome the same way.
    // First action wins: one call has one action row, and a later event sharing
    // its id (a `task_started`, or a replayed duplicate) must not displace it.
    if (!IS_OUTCOME.has(line.kind) && line.toolUseId && !byToolUse.has(line.toolUseId)) {
      byToolUse.set(line.toolUseId, row);
    }
  }
  return rows;
}
