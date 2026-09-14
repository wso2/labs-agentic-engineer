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

// THE CREW: who is doing what right now, and how long they have been quiet.
//
// The feed answers "what happened". It cannot answer "is this stuck", because a
// stall on a log surface looks exactly like a log that has scrolled — and that
// is the one question a reader of a 55-minute run actually has. So liveness is
// modelled as a property of every AGENT rather than as a line somebody has to
// find: each member carries how long since it last spoke, what it says it is
// doing, and whether that silence is explained.
//
// The rule the colours follow, and the reason for the numbers:
//
//   working  events or heartbeats are arriving, or the agent has simply not
//            been quiet long enough to be interesting.
//   waiting  blocked inside a spawned agent that is still going. NOT idle, and
//            never amber: the silence is fully explained by a row one level down.
//   stalled  STALL_MS of silence with a tool call still unanswered. 60s is twice
//            the longest normal gap in the recorded 55-minute run; the watchdog
//            that this replaces used 120s and was too slow to reassure anyone.
//   done / failed / cancelled  the runtime said so.
//
// **Silence is never a verdict.** An agent that has said nothing for ten minutes
// with no tool in flight is `working` with a ten-minute age beside it — the age
// is the honest report, and inventing a failure from it would put a red row on
// a run that is merely thinking.
//
// Pure, and takes `now` as an argument: the clock belongs to the surface (a
// React ticker, a TUI's frame loop), and a model that reads `Date.now()` cannot
// be driven to the exact instant a rule fires, which is the one thing its tests
// need to do.

import { LEAD_AGENT_ID, type AgentReport } from "./agent.js";
import {
  formatHeartbeat,
  noticeSentence,
  waitingPhrase,
  type RunEventView,
} from "./event.js";
import { groupByAgent, type AgentSection } from "./group.js";
import { laneSpans, type Interval, type LaneSpan } from "./lane.js";
import type { LineTone } from "./line.js";

/**
 * How long an agent may be silent with a tool call still unanswered before the
 * surface says so. Twice the longest normal gap measured in the recorded
 * 55-minute run (73s of that run's gaps are tool calls that did come back, and
 * its ordinary pauses sit well under 30s).
 */
export const STALL_MS = 60_000;

/** Where one crew member stands, as a reader needs to see it. */
export type CrewState =
  | "working"
  | "waiting"
  | "stalled"
  | "done"
  | "failed"
  | "cancelled";

const TERMINAL_STATES = new Set<CrewState>(["done", "failed", "cancelled"]);

/** Has this member finished, whichever way it went? */
export function isCrewSettled(state: CrewState): boolean {
  return TERMINAL_STATES.has(state);
}

/**
 * A state's semantic weight. Semantic, never a theme token — a TUI imports this
 * package too, and `warning.light` in here would make one surface's design
 * system everyone's problem.
 *
 * `waiting` weighs the same as `working` on purpose: an agent blocked inside a
 * spawned agent is not a problem, it is the fan-out working, and colouring it as
 * a warning would put an alarm beside every healthy run.
 */
export function crewTone(state: CrewState): LineTone {
  switch (state) {
    case "stalled":
    case "cancelled":
      return "warn";
    case "failed":
      return "error";
    case "done":
      return "success";
    default:
      return "info";
  }
}

/** The one word a row shows for a state, when it has no totals to show instead. */
export function crewStateLabel(state: CrewState): string {
  return state === "done" ? "completed" : state;
}

/**
 * A shell command the agent backgrounded.
 *
 * A child of the agent that STARTED it rather than a row of its own, because the
 * failure this prevents is a `dev:mock` still holding a port after the run ended
 * and nobody being able to say whose it was.
 */
export interface CrewTask {
  id: string;
  label: string;
  /** AgentStatus — `running` until a `task_settled` says otherwise. */
  status: string;
  startedAtMs?: number | undefined;
  settledAtMs?: number | undefined;
  outputBytes?: number | undefined;
}

/**
 * One entry of an agent's OWN plan — the task list the runtime keeps for itself
 * and repaints through `work_item`.
 *
 * No owner field: the entry sits on the member that owns it, so an item whose
 * owner is in doubt has already been resolved by the time anything draws it.
 */
export interface CrewPlanItem {
  id: string;
  /** The entry's subject. Empty when only status updates were ever seen for it. */
  title: string;
  /** `pending | in_progress | completed` — the runtime's task-list vocabulary. */
  status: string;
}

/**
 * A plan entry's semantic weight, the same way `crewTone` weighs a state:
 * semantic, never a theme token, because a TUI imports this package too.
 *
 * `pending` is muted on purpose — a list of things not started yet is context
 * for the row it sits under, not news.
 */
export function planTone(status: string): LineTone {
  switch (status) {
    case "in_progress":
      return "info";
    case "completed":
      return "success";
    default:
      return "muted";
  }
}

/** One agent, as the crew view and the timeline both read it. */
export interface CrewMember<E> {
  id: string;
  parentId?: string | undefined;
  /** Distance from the lead: 0 is the lead itself, 1 an agent it spawned. */
  depth: number;
  /**
   * The runtime's own report — label, role, status, totals, closing report.
   *
   * Its `activity` is deliberately cleared: the live phrase lives on `caption`
   * and nowhere else, so a surface that renders both a header and a sub-line
   * cannot print the same sentence twice.
   */
  agent: AgentReport;
  state: CrewState;
  /**
   * The one live sub-line, in the RUNTIME's words: its own phrase, the wait a
   * heartbeat declared, the condition a notice raised, or — once it is over —
   * its closing report. Empty when the runtime has said nothing at all, which a
   * surface renders as no sub-line rather than as an empty one.
   */
  caption: string;
  /**
   * Did an `agent_started` for this member arrive, or is it known only from
   * events that claimed to be it? See AgentReport.declared — and the blocking
   * rule, which is the reason this is on the member and not only on the report.
   */
  declared: boolean;
  /** The agent it is blocked inside, when it is `waiting`. */
  waitingOnId?: string | undefined;
  /** Spawned detached from its parent's turn, where the runtime said so. */
  background: boolean;
  /**
   * How long since this member last said anything. Undefined when nothing it
   * emitted carried a timestamp — a surface shows no age rather than a zero,
   * because "0s ago" about a silent agent is the one reading that must not be
   * wrong.
   */
  silentForMs?: number | undefined;
  /** Its whole life: the runtime's own total once settled, else `now` − first. */
  elapsedMs?: number | undefined;
  /** Its own steps, in order — what the inspector renders. */
  steps: E[];
  /** Shell commands it backgrounded. */
  tasks: CrewTask[];
  /**
   * Its own plan, in the order the run first mentioned each entry.
   *
   * The entries THIS agent owns, not the ones it emitted: a lead that writes a
   * list and hands an entry to an agent it spawned puts that entry on the
   * spawned agent's row, because "what was this one sent to do" is the question
   * a reader has when they click on it.
   */
  plan: CrewPlanItem[];
  /** Its lane on the run's axis, split into working and waiting stretches. */
  spans: LaneSpan[];
  children: CrewMember<E>[];
}

/** One cycle's agents, as a tree and as a flat lane order. */
export interface Crew<E> {
  lead: CrewMember<E>;
  /** Depth-first from the lead — the lane order, and the inspector's index. */
  members: CrewMember<E>[];
  /** How many agents ran, and how many still are. The hint both views share. */
  agents: number;
  running: number;
  /** How long since the NEWEST event anywhere in this cycle. */
  silentForMs?: number | undefined;
  /** The run's own ending, once it reported one. */
  outcome?: string | undefined;
  /** The axis every lane is drawn against. */
  window: { startMs: number; endMs: number };
}

// Conditions that explain a stretch of nothing happening — the ones worth
// promoting from a row to an agent's caption. A refusal or a denied write is
// news, but it is news about something that already finished; these are the
// codes that answer "why is nothing happening right now".
const SILENCE_CODES = new Set([
  "api_retry",
  "compaction",
  "rate_limit",
  "runner_scheduling",
  "runner_unschedulable",
  "runner_pulling_image",
  "runner_image_pull_backoff",
  "runner_config_error",
  "runner_starting",
  "workspace_provisioning",
  "workspace_ready",
]);

/**
 * `RunEvent.source` for an agent's OWN plan entry.
 *
 * The kind carries two populations and only this one is a plan. A `criterion` is
 * a unit of work the PLATFORM put in front of a validating run, with the
 * validation method's own statuses (`planned | exploring | … | pass | fail`);
 * folding one onto a member would paint acceptance criteria into an agent's
 * to-do list, where `completed` and `pass` do not mean the same thing.
 */
const PLAN_SOURCE = "plan";

/** A plan entry the agent removed. It is not work any more, so it is not a row. */
const PLAN_DELETED = "deleted";

/** A folded plan entry, before the crew is known well enough to place it. */
interface OwnedPlanItem extends CrewPlanItem {
  ownerAgentId: string;
}

/** What a `run_settled` outcome means for an agent that never settled itself. */
function statusFromOutcome(outcome: string): string {
  if (outcome === "failure") return "failed";
  if (outcome === "cancelled") return "stopped";
  return "completed";
}

/**
 * The earliest instant an event of this platform can honestly claim.
 *
 * A MISSING time dressed as a date is the shape this exists for, and every
 * language spells it: Go's `time.Time{}` marshals as `0001-01-01T00:00:00Z`, a
 * zero epoch is 1970, .NET's `DateTime.MinValue` is Go's again. Each is
 * perfectly well-formed, so `Date.parse` accepts it and the subtraction below
 * turns it into a duration in millennia. That happened live (2026-09-09): a
 * platform notice narrating the dark zone left its `ts` at Go's zero value, and
 * because that notice belongs to the lead, the lead's age column read
 * `1065409035m47s` — 2026 years — next to a heartbeat line that was correct.
 *
 * The floor is deliberately far below any run and far above every zero value,
 * so it can exclude no real event: this platform did not exist in 2019, and
 * nothing that ran on it can be stamped before then. An event under it
 * therefore carries no clock at all, which is a case the model already has an
 * answer for — no lane, no age, and a row all the same.
 */
const EARLIEST_EVENT_MS = Date.parse("2020-01-01T00:00:00Z");

/**
 * Epoch ms of an event's producer timestamp, or undefined if it carried none
 * the model may believe.
 *
 * The one door every clock in this file comes through, which is why the
 * plausibility rule lives here rather than beside each reading: the same stamp
 * feeds the age column, the silence, the lane and the axis both lanes and the
 * timeline are drawn against, and one implausible instant at the head of a
 * recording used to stretch that axis over two millennia — drawing every real
 * lane as a hairline.
 */
function timeOf(e: RunEventView): number | undefined {
  if (!e.ts) return undefined;
  const ms = Date.parse(e.ts);
  if (Number.isNaN(ms) || ms < EARLIEST_EVENT_MS) return undefined;
  return ms;
}

/** What one agent's events say about its timing, its silence and its tasks. */
interface Vitals {
  firstMs?: number | undefined;
  lastMs?: number | undefined;
  settledMs?: number | undefined;
  /** Unanswered NON-fan-out calls, by tool use id → the tool's name. */
  inFlight: Map<string, string>;
  /** The last `agent_progress` phrase — a replacing state, so it persists. */
  phrase: string;
  /** The last heartbeat or silence-explaining notice, and where it landed. */
  transient: string;
  transientAt: number;
  /** Where the last event that was NEITHER of those landed. */
  otherAt: number;
  tasks: Map<string, CrewTask>;
}

function newVitals(): Vitals {
  return {
    inFlight: new Map(),
    phrase: "",
    transient: "",
    transientAt: -1,
    otherAt: -1,
    tasks: new Map(),
  };
}

/**
 * One cycle's events → the crew.
 *
 * Built ON TOP of `groupByAgent` rather than beside it: the declared tree, the
 * labels and the runtime's totals are already settled there, and a second walk
 * that re-derived them is how two surfaces come to disagree about which agent
 * spawned which. This walk adds only what a tree of rows has no place for: the
 * clocks, and the repainting kinds the grouping drops — `work_item` among them,
 * which is the agent's own plan and therefore belongs to the agent rather than
 * to whichever surface happened to fold it first.
 */
export function buildCrew<E extends RunEventView>(
  events: readonly E[],
  now: number,
): Crew<E> {
  const tree = groupByAgent(events);

  const vitals = new Map<string, Vitals>();
  const vitalsFor = (agentId: string): Vitals => {
    const held = vitals.get(agentId);
    if (held) return held;
    const fresh = newVitals();
    vitals.set(agentId, fresh);
    return fresh;
  };
  // Which agent a heartbeat named, so "waiting on a spawned agent" can be shown
  // as the agent's own label — a reader can go and look at a name.
  const labels = new Map<string, string>();
  // Every live plan entry of the whole run, keyed by `itemId` and held in the
  // order the run FIRST mentioned it — a Map keeps an existing key's position
  // when it is written again, so a row does not jump when it is ticked off.
  // Run-level rather than per-agent because an entry's owner is a field on the
  // event, not the agent that emitted it.
  const planItems = new Map<string, OwnedPlanItem>();
  let outcome: string | undefined;
  let runSettledMs: number | undefined;
  let lastMs: number | undefined;

  events.forEach((event, at) => {
    const v = vitalsFor(event.agentId);
    const ms = timeOf(event);
    if (ms !== undefined) {
      v.firstMs = v.firstMs === undefined ? ms : Math.min(v.firstMs, ms);
      v.lastMs = v.lastMs === undefined ? ms : Math.max(v.lastMs, ms);
      lastMs = lastMs === undefined ? ms : Math.max(lastMs, ms);
    }
    switch (event.kind) {
      case "agent_started":
        if (event.label) labels.set(event.agentId, event.label);
        v.otherAt = at;
        return;
      case "agent_settled":
        v.settledMs = ms ?? v.settledMs;
        v.otherAt = at;
        return;
      case "agent_progress":
        // A REPLACING state, per the contract: it stands until the agent says
        // something else about itself, however many tool calls run underneath it.
        if (event.phrase) v.phrase = event.phrase;
        return;
      case "heartbeat":
        v.transient = formatHeartbeat(event, (id) => labels.get(id));
        v.transientAt = at;
        return;
      case "notice":
        // Promoted to the caption only while it is still the newest thing that
        // happened — see `captionOf`. A retry explains the current silence; the
        // same retry two tool calls later explains nothing.
        if (event.code && SILENCE_CODES.has(event.code)) {
          v.transient = noticeSentence(event);
          v.transientAt = at;
          return;
        }
        v.otherAt = at;
        return;
      case "tool_use":
        // Every tool_use here is a real call. A fan-out is not one — it has no
        // tool row at all in v2 (its `agent_started` is the row), so the stall
        // rule cannot mistake a healthy spawned agent for an unanswered command,
        // and this does not have to know any runtime's tool names to be sure.
        if (event.toolUseId) {
          v.inFlight.set(event.toolUseId, event.tool ?? "a tool call");
        }
        v.otherAt = at;
        return;
      case "tool_result":
        if (event.toolUseId) v.inFlight.delete(event.toolUseId);
        v.otherAt = at;
        return;
      case "task_started": {
        const id = event.taskId ?? String(at);
        v.tasks.set(id, {
          id,
          label: event.summary ?? event.command ?? id,
          status: "running",
          startedAtMs: ms,
        });
        v.otherAt = at;
        return;
      }
      case "task_settled": {
        // A settle whose start was never seen still gets a row: a backgrounded
        // command nobody can account for is exactly the one worth showing.
        const id = event.taskId ?? String(at);
        const open = v.tasks.get(id);
        v.tasks.set(id, {
          id,
          label: event.summary ?? open?.label ?? id,
          status: event.status ?? "completed",
          startedAtMs: open?.startedAtMs,
          settledAtMs: ms,
          outputBytes: event.outputBytes,
        });
        v.otherAt = at;
        return;
      }
      case "work_item": {
        // Still an event the agent produced, so it displaces a notice that was
        // explaining the silence — the agent is plainly doing things again.
        v.otherAt = at;
        if (event.source !== PLAN_SOURCE) return;
        // `itemId` is required of this kind by the contract. An event without
        // one is a producer this build does not understand, and is skipped
        // rather than folded into a row nothing can ever repaint.
        if (!event.itemId) return;
        const held = planItems.get(event.itemId);
        // Last status wins, because the events arrive in the order the runner
        // emitted them. The TITLE is sticky rather than overwritten by a later
        // blank: only the creating event is guaranteed to carry one (an update
        // sends a subject only when the agent renamed the entry), so taking the
        // newest would blank the row the moment the agent ticked it off. Same
        // for the owner, which a status-only update likewise omits.
        planItems.set(event.itemId, {
          id: event.itemId,
          title: event.title || held?.title || "",
          status: event.itemStatus || held?.status || "",
          ownerAgentId: event.ownerAgentId || held?.ownerAgentId || LEAD_AGENT_ID,
        });
        return;
      }
      case "run_settled":
        outcome = event.outcome ?? "success";
        runSettledMs = ms ?? runSettledMs;
        v.otherAt = at;
        return;
      default:
        v.otherAt = at;
    }
  });

  const members: CrewMember<E>[] = [];

  const build = (section: AgentSection<E>): CrewMember<E> => {
    const v = vitalsFor(section.id);
    const children = section.rows.flatMap((r) => (r.kind === "section" ? [build(r.section)] : []));
    const steps = section.rows.flatMap((r) => (r.kind === "event" ? [r.event] : []));

    // A run that has settled has settled its agents too. The contract is explicit
    // that a missing `agent_settled` means the platform never learned how an
    // agent ended — but once the run itself is over, nothing more will ever be
    // learned, and a lead left reading `running` on a finished build is the
    // clearest way to make a reader mistrust the whole surface.
    const status =
      section.agent.status === "running" && outcome
        ? statusFromOutcome(outcome)
        : section.agent.status;
    const agent: AgentReport = { ...section.agent, status, activity: undefined };
    const settledMs =
      v.settledMs ?? (status === "running" ? undefined : (runSettledMs ?? v.lastMs));

    // Blocked inside a DECLARED foreground child: a background agent is
    // detached from its parent's turn by definition, so its parent went on
    // working — and an agent the producer never announced is not a relationship
    // this surface may assume at all.
    //
    // `declared` is load-bearing and it is not a guard against one past bug. An
    // undeclared child carries no `background` field, `undefined` is not `true`,
    // so it read as a foreground spawn and held its parent in `waiting` — which
    // outranks the stall rule. That is a diagnostic being silenced by an agent
    // nobody declared and which can therefore never settle. It happened live
    // (2026-09-08: a producer minting an agent per blocking wait, so the lead
    // was `waiting` for the rest of the run and the alarm never fired), and the
    // same shape is reachable from any producer that synthesises agents from
    // first sighting — the v1 lift's `inferred` agents do exactly that. Waiting
    // is a claim, so it needs a declaration; firing a stall report slightly
    // early is the safe direction for a diagnostic, and staying quiet is not.
    const blocker = children.filter((c) => c.declared && !c.background && !isCrewSettled(c.state)).at(-1);
    const silentForMs = v.lastMs === undefined ? undefined : Math.max(0, now - v.lastMs);
    const state = stateOf(status, Boolean(blocker), v.inFlight.size > 0, silentForMs);

    const endMs = settledMs ?? (v.firstMs === undefined ? undefined : now);

    return {
      id: section.id,
      parentId: section.parentId,
      depth: section.depth,
      agent,
      state,
      caption: captionOf(state, agent, v, blocker, silentForMs, now),
      waitingOnId: state === "waiting" ? blocker?.id : undefined,
      background: section.agent.background === true,
      declared: section.agent.declared === true,
      silentForMs,
      elapsedMs: elapsedOf(agent, v, settledMs, now),
      steps,
      tasks: [...v.tasks.values()],
      // Filled once the whole crew is known — see below.
      plan: [],
      spans:
        v.firstMs === undefined || endMs === undefined
          ? []
          : laneSpans(v.firstMs, endMs, waitIntervals(children, now)),
      children,
    };
  };

  const lead = build(tree);
  // Depth-first from the lead, so the flat list reads exactly like the tree —
  // it is both the lane order and the order the inspector steps through.
  const flatten = (m: CrewMember<E>) => {
    members.push(m);
    m.children.forEach(flatten);
  };
  flatten(lead);

  // The plan is placed LAST, because placing an entry needs the whole crew: an
  // `ownerAgentId` naming an agent no event ever mentioned still belongs to
  // somebody, and it falls to the lead rather than being dropped — the same
  // reason the grouping hangs an undeclared agent off the lead. An orphaned
  // entry that silently vanishes is the one a reader most needs to see.
  const byId = new Map(members.map((m) => [m.id, m]));
  for (const item of planItems.values()) {
    if (item.status === PLAN_DELETED) continue;
    const owner = byId.get(item.ownerAgentId) ?? lead;
    owner.plan.push({ id: item.id, title: item.title, status: item.status });
  }

  const startMs = Math.min(...members.map((m) => m.spans[0]?.startMs ?? Infinity));
  const endMs = Math.max(...members.map((m) => m.spans.at(-1)?.endMs ?? -Infinity));

  return {
    lead,
    members,
    agents: members.length,
    running: members.filter((m) => !isCrewSettled(m.state)).length,
    silentForMs: lastMs === undefined ? undefined : Math.max(0, now - lastMs),
    outcome,
    window: Number.isFinite(startMs) && endMs > startMs ? { startMs, endMs } : { startMs: 0, endMs: 0 },
  };
}

/**
 * The stretches a parent spent blocked: each DECLARED foreground child's whole
 * life.
 *
 * A background child is skipped — its parent never stopped for it, and painting
 * its span on the parent's lane would charge the parent for time it spent
 * working. An UNDECLARED child is skipped for the stronger reason given at the
 * blocking rule above: nothing announced it, so no stretch of the parent's lane
 * may be attributed to waiting on it. The two rules have to agree, or a lane
 * would show a wait that the parent's own state denies.
 */
function waitIntervals<E>(children: readonly CrewMember<E>[], now: number): Interval[] {
  return children.flatMap((c) => {
    if (c.background || !c.declared) return [];
    const start = c.spans[0]?.startMs;
    if (start === undefined) return [];
    return [{ startMs: start, endMs: c.spans.at(-1)?.endMs ?? now }];
  });
}

/**
 * The one rule the colours follow.
 *
 * Order is the whole content: a settle outranks everything (a finished agent is
 * finished however quiet it went), a declared wait outranks the stall rule
 * (silence explained one level down is not a stall), and only then does a minute
 * of unanswered tool call turn a row amber.
 */
function stateOf(
  status: string,
  blocked: boolean,
  toolInFlight: boolean,
  silentForMs: number | undefined,
): CrewState {
  if (status === "failed") return "failed";
  if (status === "stopped") return "cancelled";
  if (status !== "running") return "done";
  if (blocked) return "waiting";
  if (toolInFlight && silentForMs !== undefined && silentForMs >= STALL_MS) return "stalled";
  return "working";
}

/** The runtime's own words for what this member is doing, or has done. */
function captionOf<E>(
  state: CrewState,
  agent: AgentReport,
  v: Vitals,
  blocker: CrewMember<E> | undefined,
  silentForMs: number | undefined,
  now: number,
): string {
  // A settled agent's sub-line is its REPORT. Its transcript never reaches this
  // feed and dies with the pod, so this is the only copy there will ever be.
  if (isCrewSettled(state)) return agent.report ?? "";
  // Live, and it ticks: how long the parent has been inside this call is the
  // fact a reader is actually watching, and a heartbeat's own elapsed figure
  // froze the moment it was emitted.
  if (state === "waiting" && blocker) {
    const since = blocker.spans[0]?.startMs;
    return waitingPhrase(blocker.agent.label, since === undefined ? undefined : now - since);
  }
  // A heartbeat or a retry speaks only while it is still the newest thing that
  // happened; after that the agent's own phrase is the truer line.
  const live = v.transientAt > v.otherAt && v.transient ? v.transient : v.phrase;
  if (state !== "stalled") return live;
  // An amber row must always say what it is amber ABOUT. The recorded runs carry
  // almost no heartbeats, so the unanswered call is usually the only witness —
  // and it is a true one.
  return live || waitingPhrase(v.inFlight.values().next().value ?? "a tool call", silentForMs);
}

/** How long this member has been going. */
function elapsedOf(
  agent: AgentReport,
  v: Vitals,
  settledMs: number | undefined,
  now: number,
): number | undefined {
  // The runtime's own total, where it gave one: it measured the agent's whole
  // life including the parts this feed never saw.
  if (agent.durationMs) return agent.durationMs;
  if (v.firstMs === undefined) return undefined;
  return Math.max(0, (settledMs ?? now) - v.firstMs);
}
