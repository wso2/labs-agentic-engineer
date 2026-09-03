import type { components } from "../../generated/aep-api";
import { projectTasks, validationTask, type ProjectScenario } from "./project";

type TaskView = components["schemas"]["TaskView"];
type TaskDetail = components["schemas"]["TaskDetail"];
type ExecutionView = components["schemas"]["ExecutionView"];
type TimelineEvent = components["schemas"]["TimelineEvent"];
type TaskStreamEvent = components["schemas"]["TaskStreamEvent"];

// Task detail + SSE log fixtures for the task page (#173). Each task gets a
// deterministic coding execution and, when its status has reached the build
// pipeline, a build execution; the stream handler replays these as
// TaskStreamEvent frames.

const T0 = Date.parse("2026-07-10T09:00:00Z");
const iso = (offsetSec: number) => new Date(T0 + offsetSec * 1000).toISOString();

// Task lookup for the detail/log fixtures: the list fixtures plus the
// validation task, which list-tasks excludes but get-task and the log stream
// still serve by issue number (the validation chip deep-links to it).
export function findTask(
  scenario: Exclude<ProjectScenario, "error">,
  issueNumber: number,
): TaskView | undefined {
  return (
    projectTasks[scenario].find((t) => t.issueNumber === issueNumber) ??
    (validationTask.issueNumber === issueNumber ? validationTask : undefined)
  );
}

function codingExecution(task: TaskView): ExecutionView {
  const settled = !["pending", "on_hold", "in_progress"].includes(
    task.derivedStatus,
  );
  return {
    id: `exec-${task.issueNumber}-coding`,
    kind: "coding",
    status:
      task.derivedStatus === "in_progress"
        ? "running"
        : task.derivedStatus === "abandoned"
          ? "failed"
          : "succeeded",
    createdAt: iso(0),
    startedAt: iso(5),
    ...(settled && { endedAt: iso(360) }),
  };
}

function buildExecution(task: TaskView): ExecutionView | null {
  // A validation task's merged PR spawns no build — coding is its whole story.
  if (task.executorClass === "validation") return null;
  if (!["building", "deployed", "failed"].includes(task.derivedStatus)) {
    return null;
  }
  return {
    id: `exec-${task.issueNumber}-build`,
    kind: "build",
    status:
      task.derivedStatus === "building"
        ? "running"
        : task.derivedStatus === "failed"
          ? "failed"
          : "succeeded",
    createdAt: iso(400),
    startedAt: iso(405),
    ...(task.derivedStatus !== "building" && { endedAt: iso(700) }),
  };
}

function executionsOf(task: TaskView): ExecutionView[] {
  // A pending/on-hold task has no attempts yet — exercises the
  // "waiting for the first execution" state.
  if (["pending", "on_hold"].includes(task.derivedStatus)) return [];
  const build = buildExecution(task);
  const coding = codingExecution(task);
  return build ? [coding, build] : [coding];
}

export function taskDetailOf(
  scenario: Exclude<ProjectScenario, "error">,
  issueNumber: number,
): TaskDetail | null {
  const task = findTask(scenario, issueNumber);
  if (!task) return null;
  const history = executionsOf(task);
  return {
    ...task,
    body: `Planned by the v1 build from your design.`,
    executionHistory: history,
    executions: Object.fromEntries(history.map((e) => [e.id, e])),
  };
}

// The unified timeline the stream replays, one attempt after the other.
export function taskTimeline(
  scenario: Exclude<ProjectScenario, "error">,
  issueNumber: number,
): TimelineEvent[] {
  const task = findTask(scenario, issueNumber);
  if (!task) return [];
  const lines: TimelineEvent[] = [];
  let seq = 0;
  const push = (
    executionId: string,
    executionKind: string,
    offsetSec: number,
    rest: Partial<TimelineEvent> & { kind: string },
  ) => {
    lines.push({
      schemaVersion: 1,
      ts: iso(offsetSec),
      seq: seq++,
      executionId,
      executionKind,
      ...rest,
    });
  };

  const [coding, build] = [codingExecution(task), buildExecution(task)];
  if (["pending", "on_hold"].includes(task.derivedStatus)) return lines;

  const branch = `task/${task.issueNumber}-${(task.component ?? "app").toLowerCase()}`;
  push(coding.id, "coding", 5, { kind: "phase", phase: "planning" });
  push(coding.id, "coding", 20, {
    kind: "log",
    message: `Reading specs/design/components/${task.component ?? "app"}/design.json`,
  });
  push(coding.id, "coding", 40, { kind: "phase", phase: "implementing" });
  push(coding.id, "coding", 60, {
    kind: "tool_use",
    tool: "Bash",
    command: "pnpm test",
  });
  push(coding.id, "coding", 120, {
    kind: "log",
    message: "12 tests passed, 0 failed",
  });
  push(coding.id, "coding", 180, {
    kind: "git_commit",
    sha: `3f2a1c${task.issueNumber}0`,
    files: 4,
  });
  push(coding.id, "coding", 200, { kind: "git_push", branch });

  if (task.derivedStatus === "in_progress") {
    // Live task: the stream keeps ticking after this replay.
    push(coding.id, "coding", 220, { kind: "phase", phase: "validating" });
    return lines;
  }

  if (task.derivedStatus === "abandoned") {
    push(coding.id, "coding", 240, {
      kind: "result",
      status: "failed",
      error: "issue closed before a PR merged",
    });
    return lines;
  }

  push(coding.id, "coding", 240, {
    kind: "result",
    status: "succeeded",
    summary: `PR ready on ${branch}`,
  });

  if (task.derivedStatus === "rejected") {
    push(coding.id, "coding", 300, {
      kind: "result",
      status: "failed",
      error: "PR closed without merging",
    });
    return lines;
  }
  if (!build) return lines;

  push(build.id, "build", 405, {
    kind: "build_step",
    step: "docker build",
    status: "succeeded",
  });
  push(build.id, "build", 500, {
    kind: "build_step",
    step: "push image",
    status: task.derivedStatus === "failed" ? "failed" : "succeeded",
  });
  if (task.derivedStatus === "failed") {
    push(build.id, "build", 520, {
      kind: "result",
      status: "failed",
      error: "image push denied: registry credentials expired",
    });
  } else if (task.derivedStatus === "deployed") {
    push(build.id, "build", 640, {
      kind: "gh_action",
      step: "deploy-dev",
      status: "succeeded",
    });
    push(build.id, "build", 700, {
      kind: "result",
      status: "succeeded",
      summary: "deployed to dev",
    });
  }
  return lines;
}

// Terminal derivedStatus values end the mock stream with a `done` frame.
export function isSettledStatus(derivedStatus: string): boolean {
  return ["deployed", "failed", "rejected", "abandoned"].includes(
    derivedStatus,
  );
}

export function streamFrames(
  scenario: Exclude<ProjectScenario, "error">,
  issueNumber: number,
): TaskStreamEvent[] {
  const task = findTask(scenario, issueNumber);
  if (!task) return [];
  const frames: TaskStreamEvent[] = [{ type: "task", task }];
  for (const execution of executionsOf(task)) {
    frames.push({ type: "execution", execution });
  }
  for (const line of taskTimeline(scenario, issueNumber)) {
    frames.push({ type: "line", line });
  }
  if (isSettledStatus(task.derivedStatus)) {
    frames.push({ type: "done", derivedStatus: task.derivedStatus });
  }
  return frames;
}

// For live tasks the mock keeps emitting a heartbeat line every few seconds
// so the console visibly streams; `seq` continues from the replayed tail.
export function liveLine(
  issueNumber: number,
  seq: number,
  tick: number,
): TimelineEvent {
  return {
    schemaVersion: 1,
    ts: iso(220 + tick * 4),
    seq,
    kind: "log",
    executionId: `exec-${issueNumber}-coding`,
    executionKind: "coding",
    message: `checking validation criteria (${tick})…`,
  };
}
