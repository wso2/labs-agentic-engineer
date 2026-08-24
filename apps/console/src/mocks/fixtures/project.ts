import type { components } from "../../generated/aep-api";
import { taskUsage } from "./usage";
import {
  DEFAULT_VALIDATION_CRITERIA,
  DEFAULT_VALIDATION_REPORT,
} from "./validation";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type ComponentList = components["schemas"]["ComponentList"];
type ComponentOpenAPI = components["schemas"]["ComponentOpenAPI"];
type TaskView = components["schemas"]["TaskView"];
type TagList = components["schemas"]["TagList"];
type BuildList = components["schemas"]["BuildList"];
type BuildRunList = components["schemas"]["BuildRunList"];
type MilestoneRunView = components["schemas"]["MilestoneRunView"];
type CycleBuild = components["schemas"]["CycleBuild"];
type DeploymentList = components["schemas"]["DeploymentList"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type FileMeta = components["schemas"]["FileMeta"];
type FileContent = components["schemas"]["FileContent"];
type ApiError = components["schemas"]["Error"];

// Scenario switch for the project overview (#77/#183) and spec view (#80).
// Toggle in devtools:
//   localStorage.setItem('aep:mock:project',
//     'fresh' | 'spec' | 'spec-failed' | 'building' | 'deploying' |
//     'deployed' | 'deploy-failed' | 'repo-error' | 'error')
export type ProjectScenario =
  | "fresh"
  | "spec"
  | "spec-failed"
  | "building"
  | "deploying"
  | "deployed"
  | "deploy-failed"
  | "repo-error"
  | "error";

const REPO_URL = "https://github.com/acme-dev/demo-shop";
const BOARD_URL = "https://github.com/acme-dev/demo-shop/issues";

// Stage-aggregate shorthands (#183/#184). The overview pipeline renders
// exclusively from these.
type SpecStage = components["schemas"]["SpecStage"];
type BuildStage = components["schemas"]["BuildStage"];
type DeployStage = components["schemas"]["DeployStage"];

const noSpec: SpecStage = {
  exists: false,
  version: "",
  dirty: false,
  design: false,
};
const idleBuild: BuildStage = { version: "", status: "idle" };
const noDeploy: DeployStage = {
  version: "",
  status: "none",
  components: { total: 0, ready: 0 },
  validation: "none",
};

// The phase ladder these carry stops at "tasks", exactly as the server's does
// (aep-api `status_stages.go`): nothing has emitted "components" since tasks
// became GitHub issues, and hasTasks is always false there — a live count on a
// 5s poll is not worth the GitHub request. Fixtures that ran ahead of the
// server hid a real bug: the header chip read "Active" under MSW and "Building"
// forever against the API. Mirror the server, and let the stage aggregates
// carry the scenario.
export const projectStatuses: Record<
  Exclude<ProjectScenario, "error">,
  ProjectStatus
> = {
  // Just created from a prompt: spec derivation hasn't produced anything.
  fresh: {
    phase: "prompt",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: false,
    hasDesign: false,
    hasTasks: false,
    specStatus: "pending",
    designStatus: "pending",
    spec: noSpec,
    build: idleBuild,
    deploy: noDeploy,
  },
  // Spec collaboration underway, nothing published.
  spec: {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: false,
    specStatus: "draft",
    designStatus: "in_progress",
    spec: { exists: true, version: "", dirty: false, design: true },
    build: idleBuild,
    deploy: noDeploy,
  },
  // Spec derivation hit a problem; the seeded PRD is still there.
  "spec-failed": {
    phase: "spec",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: false,
    hasTasks: false,
    specStatus: "failed",
    designStatus: "failed",
    spec: { exists: true, version: "", dirty: false, design: false },
    build: idleBuild,
    deploy: noDeploy,
  },
  // v1 published, agents building, nothing deployed yet. Task counts mirror
  // buildingTasks below (1 failed, 3 still moving).
  building: {
    phase: "tasks",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: false,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true },
    build: {
      version: "v1",
      status: "running",
    },
    deploy: noDeploy,
  },
  // v1 built, dev rollout in progress (1 of 3 components ready).
  deploying: {
    phase: "tasks",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: false,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true },
    build: {
      version: "v1",
      status: "succeeded",
    },
    deploy: {
      version: "v1",
      status: "deploying",
      components: { total: 3, ready: 1 },
      validation: "none",
    },
  },
  // v1 deployed to dev; spec has drifted since (dirty → rendered v1+).
  deployed: {
    phase: "tasks",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: false,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: true, design: true },
    build: {
      version: "v1",
      status: "succeeded",
    },
    deploy: {
      version: "v1",
      status: "deployed",
      components: { total: 3, ready: 3 },
      // Mirrors settledRun's verdict below — the chip and the run story are read
      // side by side on the deployments board, so they cannot disagree here.
      validation: "partial",
    },
  },
  // v1 build done but the dev deployment failed.
  "deploy-failed": {
    phase: "tasks",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: false,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true },
    build: {
      version: "v1",
      status: "succeeded",
    },
    deploy: {
      version: "v1",
      status: "failed",
      components: { total: 3, ready: 1 },
      validation: "none",
    },
  },
  // Repo bootstrap went sideways before any spec work.
  "repo-error": {
    phase: "repo-error",
    repoStatus: "error",
    repoErrorMessage: "GitHub App installation lacks repo-creation permission",
    repoUrl: "",
    hasSpec: false,
    hasDesign: false,
    hasTasks: false,
    specStatus: "pending",
    designStatus: "pending",
    spec: noSpec,
    build: idleBuild,
    deploy: noDeploy,
  },
};

const emptyComponents: ComponentList = { items: [] };

const builtComponents: ComponentList = {
  items: [
    {
      name: "storefront",
      displayName: "Storefront",
      description: "Customer-facing web app",
      type: "web-application",
      status: "active",
    },
    {
      name: "catalog-api",
      displayName: "Catalog API",
      description: "Product catalog service",
      type: "service",
      status: "active",
    },
    {
      name: "orders-api",
      displayName: "Orders API",
      description: "Order processing service",
      type: "service",
      status: "active",
    },
  ],
};

// Note: no endpointUrl on the components themselves — the real backend never
// fills Component.endpointUrl (noted drift, #196); the console reads a web
// app's URL from list-deployments (componentDeployments below).
const deployedComponents: ComponentList = builtComponents;

// Deployments backing list-deployments — shared by the overview's "Open
// app" link (#196) and the Deployments board (#216, one fan-out call per
// component). Per component × scenario; components absent from a scenario
// render as greyed "Not deployed" cards on the board, and the distinguished
// status "Undeployed" marks an intentional spec.state == Undeploy binding.
const deploymentsByScenario: Partial<
  Record<
    Exclude<ProjectScenario, "error">,
    Record<string, DeploymentList["items"]>
  >
> = {
  // One distinct state per component, so the board demos the full chip
  // vocabulary: transitional, settled success, and a fresh binding whose
  // conditions haven't reported yet ("Pending").
  deploying: {
    storefront: [
      {
        name: "demo-shop-storefront-development",
        componentName: "storefront",
        environment: "development",
        status: "Progressing",
        releaseName: "demo-shop-storefront-a1b2c3",
        createdAt: "2026-07-12T05:04:00Z",
      },
    ],
    "catalog-api": [
      {
        name: "demo-shop-catalog-api-development",
        componentName: "catalog-api",
        environment: "development",
        status: "Ready",
        releaseName: "demo-shop-catalog-api-d4e5f6",
        endpointUrl: "https://catalog-api.dev.acme-aep.io",
        createdAt: "2026-07-12T04:58:00Z",
      },
    ],
    "orders-api": [
      {
        name: "demo-shop-orders-api-development",
        componentName: "orders-api",
        environment: "development",
        createdAt: "2026-07-12T05:05:30Z",
      },
    ],
  },
  deployed: {
    storefront: [
      {
        name: "demo-shop-storefront-development",
        componentName: "storefront",
        environment: "development",
        status: "Ready",
        releaseName: "demo-shop-storefront-a1b2c3",
        endpointUrl: "https://storefront.dev.acme-aep.io",
        createdAt: "2026-07-12T05:04:00Z",
      },
    ],
    "catalog-api": [
      {
        name: "demo-shop-catalog-api-development",
        componentName: "catalog-api",
        environment: "development",
        status: "Ready",
        releaseName: "demo-shop-catalog-api-d4e5f6",
        endpointUrl: "https://catalog-api.dev.acme-aep.io",
        createdAt: "2026-07-12T04:58:00Z",
      },
    ],
    // Settled but intentionally undeployed — the "deployed" scenario stays
    // all-settled while still showing the Undeployed chip.
    "orders-api": [
      {
        name: "demo-shop-orders-api-development",
        componentName: "orders-api",
        environment: "development",
        status: "Undeployed",
        createdAt: "2026-07-12T05:01:00Z",
      },
    ],
  },
  "deploy-failed": {
    storefront: [
      {
        name: "demo-shop-storefront-development",
        componentName: "storefront",
        environment: "development",
        status: "ReleaseFailed",
        releaseName: "demo-shop-storefront-a1b2c3",
        createdAt: "2026-07-12T05:04:00Z",
      },
    ],
    "catalog-api": [
      {
        name: "demo-shop-catalog-api-development",
        componentName: "catalog-api",
        environment: "development",
        status: "Ready",
        releaseName: "demo-shop-catalog-api-d4e5f6",
        endpointUrl: "https://catalog-api.dev.acme-aep.io",
        createdAt: "2026-07-12T04:58:00Z",
      },
    ],
    // Still converging when the storefront's release failed — a mixed
    // mid-rollout picture (error + success + transitional).
    "orders-api": [
      {
        name: "demo-shop-orders-api-development",
        componentName: "orders-api",
        environment: "development",
        status: "Progressing",
        releaseName: "demo-shop-orders-api-g7h8i9",
        createdAt: "2026-07-12T05:01:00Z",
      },
    ],
  },
};

export function componentDeployments(
  s: Exclude<ProjectScenario, "error">,
  componentName: string,
): DeploymentList {
  return { items: deploymentsByScenario[s]?.[componentName] ?? [] };
}

// Design dependencies backing list-design-dependencies — the Spec view's
// status chips and the Deployments page's promotion connections. Shared
// dependencies are declared on EVERY consuming component's own list, exactly
// as the real read model serves them (the console dedupes). The auth app
// carries config defaults (so it demos as "Set"), stripe carries none (so
// the promote dialog has a required connection to collect).
const sharedAuthDependency = {
  kind: "platform-resource",
  name: "shop-auth",
  resourceType: "thunder-app",
  config: [
    {
      key: "TENANT_DOMAIN",
      description: "Tenant domain",
      defaultValue: "auth.demo-shop.dev",
    },
    {
      key: "CLIENT_SECRET",
      description: "Client secret",
      secret: true,
      defaultValue: "dev-client-secret",
    },
  ],
};

const designDependencies: ComponentDependencies[] = [
  {
    componentName: "storefront",
    dependencies: [
      { kind: "component", name: "catalog-api" },
      { kind: "component", name: "orders-api" },
      sharedAuthDependency,
    ],
  },
  {
    componentName: "catalog-api",
    dependencies: [
      {
        kind: "platform-resource",
        name: "shop-db",
        resourceType: "postgres-cnpg",
      },
    ],
  },
  {
    componentName: "orders-api",
    dependencies: [
      {
        kind: "platform-resource",
        name: "shop-db",
        resourceType: "postgres-cnpg",
      },
      sharedAuthDependency,
      {
        kind: "external",
        name: "stripe",
        config: [
          { key: "STRIPE_SECRET_KEY", description: "Secret key", secret: true },
          {
            key: "STRIPE_WEBHOOK_SECRET",
            description: "Webhook signing secret",
            secret: true,
          },
        ],
      },
    ],
  },
];

export function projectDependencies(
  s: Exclude<ProjectScenario, "error">,
): ComponentDependencies[] {
  // No design yet, nothing to declare dependencies.
  return s === "fresh" || s === "repo-error" ? [] : designDependencies;
}

// The OpenAPI contract served by GET .../components/:name/openapi — a
// `{ spec }` envelope carrying a raw document, exactly as aep-api returns it
// (read off specs/design). The ComponentOpenApiDialog renders `spec` via the
// shared OpenApiView. Title is keyed off the component so the viewer's hero
// reflects which row was opened.
export function componentOpenApi(componentName: string): ComponentOpenAPI {
  const spec = `openapi: 3.0.0
info:
  title: ${componentName}
  version: 1.0.0
  description: Mock API contract for ${componentName}.
paths:
  /health:
    get:
      summary: Health check
      responses:
        "200":
          description: OK
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: A list of items
    post:
      summary: Create an item
      responses:
        "201":
          description: Created
`;
  return { componentName, componentType: "service", spec };
}

export const projectComponents: Record<
  Exclude<ProjectScenario, "error">,
  ComponentList
> = {
  fresh: emptyComponents,
  spec: emptyComponents,
  "spec-failed": emptyComponents,
  building: builtComponents,
  deploying: builtComponents,
  deployed: deployedComponents,
  "deploy-failed": builtComponents,
  "repo-error": emptyComponents,
};

// Issues backing list-tasks. `derivedStatus` is the whole vocabulary the
// platform has after the flip: the GitHub issue is open (pending) or closed
// (merged). `executorClass` is the label-derived kind the Builds page sections
// on — agent work, a dispatch gate, or a bare human ledger issue.
function task(
  issueNumber: number,
  title: string,
  derivedStatus: "pending" | "merged",
  executorClass: "coding" | "provision" | "ledger" = "coding",
  component?: string,
): TaskView {
  const usage = taskUsage[issueNumber];
  return {
    issueNumber,
    title,
    derivedStatus,
    executorClass,
    issueUrl: `${BOARD_URL}/${issueNumber}`,
    ...(component !== undefined && { component }),
    ...(usage !== undefined && { usage }),
    attention: null,
    dependsOn: null,
    executions: {},
    hold: false,
    lineage: { specTag: "v1" },
  };
}

// No validation issue here: list-tasks hides it — it is a phase of the run and
// surfaces with the run's verdict on the deployment surface.
const buildingTasks: TaskView[] = [
  // An open dispatch gate, with the provisioning run the platform admitted
  // against it — the ordinary mid-build state, and the one the run card must
  // read as work in progress rather than as a hold on the user. It renders as
  // a tagged ROW here as well: a provisioned connection is part of the
  // version's record, not just a reason nothing is moving.
  {
    ...task(
      8,
      "Provision resource: orders-db (postgres-cnpg)",
      "pending",
      "provision",
    ),
    executions: {
      provision: {
        id: "exec-provision-8",
        kind: "provision",
        status: "running",
        createdAt: "2026-07-10T09:12:30Z",
        startedAt: "2026-07-10T09:12:35Z",
      },
    },
  },
  task(
    12,
    "Checkout flow with cart persistence",
    "pending",
    "coding",
    "storefront",
  ),
  task(
    10,
    "Product catalog CRUD endpoints",
    "pending",
    "coding",
    "catalog-api",
  ),
  task(9, "Scaffold storefront app shell", "merged", "coding", "storefront"),
  task(
    11,
    "Orders service payment integration",
    "pending",
    "coding",
    "orders-api",
  ),
  // Filed by a human against this version: ledger only, never worked.
  task(21, "Checkout is slow on mobile", "pending", "ledger"),
];

const doneTasks: TaskView[] = buildingTasks.map((t) => ({
  ...t,
  // The gate resolved and every task landed; the ledger issue is still open,
  // because a ledger issue never stalls settle.
  derivedStatus: t.executorClass === "ledger" ? "pending" : "merged",
}));

// The version's ONE validation issue: kept OUT of projectTasks — list-tasks
// hides it — but get-task still serves it by number. Its verdict is a RUN
// property and lives on the run rows, which is where the deployment surface
// reads it.
export const validationTask: TaskView = {
  ...task(30, "Validate deployed system against acceptance criteria", "merged"),
  executorClass: "validation",
};

export const projectTasks: Record<
  Exclude<ProjectScenario, "error">,
  TaskView[]
> = {
  fresh: [],
  spec: [],
  "spec-failed": [],
  building: buildingTasks,
  deploying: doneTasks,
  deployed: doneTasks,
  "deploy-failed": doneTasks,
  "repo-error": [],
};

// Builds backing list-project-builds — the version ledger the builds page
// reads (#185): one entry per built spec version, newest first, each carrying
// the state of the newest milestone run that has worked it.
const noBuilds: BuildList = { builds: [] };
const runningV1Build: BuildList = {
  builds: [
    {
      tag: "v1",
      milestoneNumber: 1,
      status: "in_progress",
      startedAt: "2026-07-10T09:12:00Z",
    },
  ],
};
const completedV1Build: BuildList = {
  builds: [
    {
      tag: "v1",
      milestoneNumber: 1,
      status: "completed",
      startedAt: "2026-07-10T09:12:00Z",
      completedAt: "2026-07-10T10:03:00Z",
    },
  ],
};

// Milestone runs backing list-build-runs — the version's whole story: run rows
// and their cycle records, DB-only on the server. Branch, PR number and merge
// SHA are LEARNED FROM WEBHOOKS, so the in-flight cycle carries none of them.
function milestoneRun(over: Partial<MilestoneRunView> = {}): MilestoneRunView {
  return {
    id: "run-v1-1",
    milestoneNumber: 1,
    milestoneTitle: "v1",
    kind: "dev",
    origin: "spec-build",
    state: "running",
    budgets: {
      cyclesTotal: 2,
      cycleCeiling: 8,
      fixCycles: 1,
      conflictCycles: 0,
      buildRetriggers: 1,
      validationCycles: 1,
    },
    validation: {},
    cycles: [
      {
        id: "cycle-1",
        kind: "coding",
        attempts: 1,
        branch: "aep/m1-c1",
        prNumber: 3,
        prUrl: `${REPO_URL}/pull/3`,
        // The merge policy's matched set — what this session's pull request
        // claimed, and therefore what its merge closed. #9 is closed in the
        // issue plane, which is exactly why the set has to be recorded: nothing
        // else can attribute a closed issue to the session that closed it.
        resolves: [9],
        mergeSha: "dcb1edc5fe0417b2",
        createdAt: "2026-07-10T09:14:00Z",
        endedAt: "2026-07-10T09:41:00Z",
      },
      {
        id: "cycle-2",
        kind: "fix",
        attempts: 2,
        createdAt: "2026-07-10T09:45:00Z",
      },
    ],
    createdAt: "2026-07-10T09:12:00Z",
    startedAt: "2026-07-10T09:13:00Z",
    ...over,
  };
}

const noRuns: BuildRunList = { tag: "v1", milestoneNumber: 1, runs: [] };
const liveRun: BuildRunList = {
  tag: "v1",
  milestoneNumber: 1,
  runs: [milestoneRun()],
};
// A parked run: the state cancel exists for, so the mock can show the banner.
const waitingRun: BuildRunList = {
  tag: "v1",
  milestoneNumber: 1,
  runs: [milestoneRun({ state: "waiting" })],
};
// A run that SELF-HEALED: its first validation attempt failed, the platform filed
// the failed criterion as ordinary work, a coding cycle repaired it, and the second
// attempt came back clean. Four cycles — coding, validation, coding, validation —
// with a verdict on each attempt, because the run's own verdict is only its latest.
//
// The shape matters to the console beyond looking realistic: the report is read at
// the LAST validation cycle's merge commit, so a fixture with two of them is what
// catches a reader that takes the first.
//
// The verdict is `partial`, not `passed`, because the default oracle carries a
// manual and a scenario criterion — methods no runner executes. `passed` REQUIRES
// full coverage, so claiming it here would have the tile say "all criteria passed"
// over a report showing two nobody checked. Every other verdict is one devtools
// key away: see fixtures/validation.ts.
const settledRun: BuildRunList = {
  tag: "v1",
  milestoneNumber: 1,
  runs: [
    milestoneRun({
      state: "succeeded",
      endedAt: "2026-07-10T10:41:00Z",
      validation: {
        verdict: "partial",
        issue: 30,
        reportPath: "tests/validation/report.json",
      },
      cycles: [
        {
          id: "cycle-1",
          kind: "coding",
          attempts: 1,
          branch: "aep/m1-c1",
          prNumber: 3,
          prUrl: `${REPO_URL}/pull/3`,
          resolves: [9],
          mergeSha: "dcb1edc5fe0417b2",
          createdAt: "2026-07-10T09:14:00Z",
          endedAt: "2026-07-10T09:41:00Z",
        },
        {
          id: "cycle-2",
          kind: "validation",
          attempts: 1,
          branch: "aep/m1-c2",
          prNumber: 4,
          prUrl: `${REPO_URL}/pull/4`,
          mergeSha: "5c0de1a77b3f2049",
          validationVerdict: "failed",
          validationIssue: 30,
          createdAt: "2026-07-10T09:45:00Z",
          endedAt: "2026-07-10T10:02:00Z",
        },
        // The repair: an ordinary coding cycle over the repair issue the failed
        // attempt filed. No "repair" kind exists, because a repair is ordinary work.
        {
          id: "cycle-3",
          kind: "coding",
          attempts: 1,
          branch: "aep/m1-c3",
          prNumber: 5,
          prUrl: `${REPO_URL}/pull/5`,
          resolves: [13],
          mergeSha: "9f2ab4c81de60357",
          createdAt: "2026-07-10T10:05:00Z",
          endedAt: "2026-07-10T10:21:00Z",
        },
        {
          id: "cycle-4",
          kind: "validation",
          attempts: 1,
          branch: "aep/m1-c4",
          prNumber: 6,
          prUrl: `${REPO_URL}/pull/6`,
          mergeSha: "7ab41c90ee31d5f0",
          validationVerdict: "partial",
          validationIssue: 30,
          createdAt: "2026-07-10T10:24:00Z",
          endedAt: "2026-07-10T10:40:00Z",
        },
      ],
    }),
    // An EARLIER session of the same milestone: a cancelled incident whose one
    // cycle never opened a pull request. Gives the builds page's history list
    // something to collapse, and its detail something to show.
    milestoneRun({
      id: "run-0",
      kind: "task",
      origin: "incident-adoption",
      state: "cancelled",
      terminalReason: "cancelled",
      createdAt: "2026-07-10T06:25:00Z",
      startedAt: "2026-07-10T06:25:00Z",
      endedAt: "2026-07-10T09:12:00Z",
      cycles: [
        {
          id: "run0-cycle-1",
          kind: "coding",
          attempts: 2,
          branch: "aep/m1-r0c1",
          createdAt: "2026-07-10T06:30:00Z",
          endedAt: "2026-07-10T09:10:00Z",
        },
      ],
    }),
  ],
};

export const projectBuildRuns: Record<
  Exclude<ProjectScenario, "error">,
  BuildRunList
> = {
  fresh: noRuns,
  spec: noRuns,
  "spec-failed": noRuns,
  // A gate is open in the `building` scenario's issue list, so its run is
  // parked — which is exactly when the hold notice and cancel both matter.
  building: waitingRun,
  deploying: liveRun,
  deployed: settledRun,
  "deploy-failed": settledRun,
  "repo-error": noRuns,
};

// The build fan-out one build session's merge produced — the Builds stage of the
// run's rail, and the Deployment stage that reads its verdict.
//
// `status` is OpenChoreo's condition Reason carried verbatim, and `completed` is
// the only terminal gate, so a fixture must set both rather than implying one
// from the other. `attempt` above 1 is the single automatic re-trigger a red
// build gets per (component, SHA).
function cycleBuild(
  component: string,
  status: string,
  completed: boolean,
  attempt = 1,
): CycleBuild {
  return {
    component,
    buildName: `demo-shop-${component}-dcb1edc5fe04-${attempt}`,
    status,
    completed,
    attempt,
    startedAt: "2026-07-10T09:41:30Z",
  };
}

const greenFanOut: CycleBuild[] = [
  cycleBuild("storefront", "Succeeded", true),
  cycleBuild("catalog-api", "Succeeded", true),
];

// One component still red after its re-trigger: the case where a session lands
// its merge and still delivers nothing, and the fix issue comes back into the
// milestone. This is what makes the `deploy-failed` scenario's name true — it
// used to serve the same all-green fan-out as `deployed`.
const redFanOut: CycleBuild[] = [
  cycleBuild("storefront", "Succeeded", true),
  cycleBuild("catalog-api", "Failed", true, 2),
];

const movingFanOut: CycleBuild[] = [
  cycleBuild("storefront", "Running", false),
  // OpenChoreo's word for a run that exists but has not started.
  cycleBuild("catalog-api", "Pending", false),
];

export const projectCycleBuilds: Record<
  Exclude<ProjectScenario, "error">,
  CycleBuild[]
> = {
  fresh: [],
  spec: [],
  "spec-failed": [],
  building: movingFanOut,
  deploying: movingFanOut,
  deployed: greenFanOut,
  "deploy-failed": redFanOut,
  "repo-error": [],
};

export const projectBuilds: Record<
  Exclude<ProjectScenario, "error">,
  BuildList
> = {
  fresh: noBuilds,
  spec: noBuilds,
  "spec-failed": noBuilds,
  building: runningV1Build,
  deploying: completedV1Build,
  deployed: completedV1Build,
  "deploy-failed": completedV1Build,
  "repo-error": noBuilds,
};

// Spec version tags (#117): latest = newest user tag; specDirty = specs/
// moved on GitHub since. The 'deployed' scenario drifts to exercise the
// "draft changes" chip.
const noTags: TagList = { tags: [] };
const v1Tags: TagList = { tags: ["v1"], latest: "v1", specDirty: false };

export const projectTags: Record<Exclude<ProjectScenario, "error">, TagList> = {
  fresh: noTags,
  spec: noTags,
  "spec-failed": noTags,
  building: v1Tags,
  deploying: v1Tags,
  deployed: { ...v1Tags, specDirty: true },
  "deploy-failed": v1Tags,
  "repo-error": noTags,
};

// Spec bundle backing the spec view (#80). The backend seeds a PRD at repo
// initialization, so requirements is never empty; designs/validation fill
// in as the agents derive them.
const seededPrd = `# Demo Shop — PRD

## Goal

A small storefront where customers browse the product catalog, add items
to a cart, and check out.

## Requirements

- Browse products by category with search.
- Cart persists across sessions.
- Checkout with a mocked payment provider.
- Order history per customer.
`;

const userStories = `# Demo Shop — User stories

- As a shopper, I can search the catalog so that I find products quickly.
- As a shopper, my cart survives a page reload so that I don't lose picks.
- As a shopper, I can check out and see an order confirmation.
- As a returning customer, I can see my past orders.
`;

const architectureMd = `# Demo Shop — Component architecture

Three components behind the project cell:

| Component | Type | Responsibility |
|---|---|---|
| storefront | web-application | Customer-facing UI |
| catalog-api | service | Product catalog CRUD + search |
| orders-api | service | Cart, checkout, order history |

The storefront talks to both services; services share nothing.
`;

// Per-component design files (#80 rich design view): design.json for each of
// the three components named in architectureMd, plus one wireframes.dsl for
// the customer-facing component — enough to exercise the Designs sidebar's
// component grouping and the derived Architecture / Wireframe views.
const storefrontDesignJson = `{
  "name": "storefront",
  "type": "web-application",
  "version": "0.1.0",
  "language": "TypeScript",
  "buildpack": "nodejs",
  "appPath": "storefront",
  "entrypoint": "src/main.tsx",
  "exposure": "internet",
  "description": "Customer-facing storefront UI.",
  "dependencies": [
    { "kind": "component", "name": "catalog-api" },
    { "kind": "component", "name": "orders-api" }
  ]
}`;

const storefrontWireframesDsl = `screen Catalog "Shoppers browse and search the product catalogue"
  navbar "Demo Shop | Catalog | Cart | Orders | Account"
  row
    heading "Browse products"
    right
    search "Search products, brands, SKUs"
    select "Category: All"
  tabs "All | New in | On sale | Bestsellers"
  row
    card "Wireless Headphones\n$89"
      badge "In stock" success
    card "Mechanical Keyboard\n$129"
      badge "Low stock" warning
    card "4K Monitor\n$349"
      badge "In stock" success
    card "USB-C Hub\n$39"
      badge "In stock" success
  row
    right
    button "View cart" primary -> Cart

screen Cart "Shopper reviews items and checks out"
  navbar "Demo Shop | Catalog | Cart | Orders | Account"
  heading "Your cart"
  split 60/40
    left
      table "Product | Qty | Price | Subtotal"
        row "Wireless Headphones | 1 | $89.00 | $89.00"
        row "USB-C Hub | 2 | $39.00 | $78.00"
        row "Mechanical Keyboard | 1 | $129.00 | $129.00"
      button "Continue shopping" -> Catalog
    right
      card "Order summary"
        text "Subtotal: $296.00"
        text "Shipping: $6.00"
        text "Total: $302.00"
        checkbox "Ship to billing address" active
        button "Checkout" primary -> Orders

screen Orders "Shopper tracks past orders and their status"
  navbar "Demo Shop | Catalog | Cart | Orders | Account"
  heading "Your orders"
  table "Order | Placed | Items | Total | Status"
    row "#10432 | Jul 8, 2026 | 3 | $302.00 | Shipped"
    row "#10391 | Jun 27, 2026 | 1 | $89.00 | Delivered"
    row "#10355 | Jun 15, 2026 | 2 | $168.00 | Delivered"

// Two journeys over the same three screens, so mock mode exercises every
// flow case the prototype has to render: a screen in one flow (Cart, Orders),
// a screen both flows reach (Catalog → "Common" on the canvas), and a flow
// picker with more than one entry.
flow "Browse & buy"
  role "Shopper"
  description "A shopper finds products and checks out"
  Catalog
  Cart

flow "Order tracking"
  role "Shopper"
  description "A signed-in shopper checks where a placed order is"
  Catalog
  Orders
`;

const catalogApiDesignJson = `{
  "name": "catalog-api",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "catalog-api",
  "entrypoint": "cmd/main",
  "exposure": "intranet",
  "description": "Product catalog CRUD + search.",
  "dependencies": []
}`;

const ordersApiDesignJson = `{
  "name": "orders-api",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "orders-api",
  "entrypoint": "cmd/main",
  "exposure": "intranet",
  "description": "Cart, checkout, and order history.",
  "dependencies": []
}`;

const validationPlan = `# Demo Shop — Validation plan

- Catalog search returns seeded products by name and category.
- Cart contents survive a browser restart.
- Checkout produces an order visible in order history.
- Each service exposes /healthz returning 200.
`;

// The two validation artifacts come from ./validation.ts, which builds the oracle
// and the run report from ONE outcome map so they cannot disagree — and which the
// aep:mock:validation switch swaps wholesale to reach every verdict.

// Spec files as the Files API serves them (#113): repo-relative paths under
// specs/, metadata (list-files) split from content (read-file). Text only —
// the Files API carries no binary (ADR-0017 took reference documents out of
// git, and with them the base64 encoding field).
interface MockSpecFile {
  path: string;
  content: string;
}

const prdOnlyFiles: MockSpecFile[] = [
  { path: "specs/requirements/prd.md", content: seededPrd },
];

const collaborationFiles: MockSpecFile[] = [
  ...prdOnlyFiles,
  { path: "specs/requirements/user-stories.md", content: userStories },
  { path: "specs/design/architecture.md", content: architectureMd },
];

const fullFiles: MockSpecFile[] = [
  ...collaborationFiles,
  {
    path: "specs/design/components/storefront/design.json",
    content: storefrontDesignJson,
  },
  {
    path: "specs/design/components/storefront/wireframes.dsl",
    content: storefrontWireframesDsl,
  },
  {
    path: "specs/design/components/catalog-api/design.json",
    content: catalogApiDesignJson,
  },
  {
    path: "specs/design/components/orders-api/design.json",
    content: ordersApiDesignJson,
  },
  { path: "specs/validation/validation-plan.md", content: validationPlan },
  {
    path: "specs/validation/validation-criteria.json",
    content: DEFAULT_VALIDATION_CRITERIA,
  },
  // Runner artifact outside specs/ — reachable via the read-file allow-list.
  { path: "tests/validation/report.json", content: DEFAULT_VALIDATION_REPORT },
];

export const projectSpecFiles: Record<
  Exclude<ProjectScenario, "error">,
  MockSpecFile[]
> = {
  fresh: prdOnlyFiles,
  spec: collaborationFiles,
  "spec-failed": prdOnlyFiles,
  building: fullFiles,
  deploying: fullFiles,
  deployed: fullFiles,
  "deploy-failed": fullFiles,
  "repo-error": prdOnlyFiles,
};

// Deterministic stand-in for the git blob sha: stable per content revision,
// so the console's (path, sha) content caching behaves like it does live.
function mockSha(input: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0").repeat(5);
}

export function specFileMetas(files: MockSpecFile[]): FileMeta[] {
  return files
    .map((f) => ({
      path: f.path,
      sha: mockSha(f.path + f.content),
      size: byteLength(f.content),
    }))
    .sort((a, b) => a.path.localeCompare(b.path));
}

export function specFileContent(
  files: MockSpecFile[],
  path: string,
): FileContent | null {
  const file = files.find((f) => f.path === path);
  if (!file) return null;
  return {
    path: file.path,
    content: file.content,
    sha: mockSha(file.path + file.content),
  };
}

export const specFileNotFound = (path: string): ApiError => ({
  code: "not_found",
  message: `no spec file at ${path}`,
});

// Files written through the mock files/apply. Persisted per project to
// localStorage (like created projects) so the Spec view still lists them after
// a reload. Reference documents no longer come through here at all — they go
// to the references endpoint and are never committed (ADR-0017).
const APPLIED_FILES_KEY = "aep:mock:appliedFiles";

interface AppliedFile extends MockSpecFile {
  size: number;
  sha: string;
}

type AppliedFilesByProject = Record<string, AppliedFile[]>;

function loadAppliedFiles(): AppliedFilesByProject {
  try {
    const raw = localStorage.getItem(APPLIED_FILES_KEY);
    return raw ? (JSON.parse(raw) as AppliedFilesByProject) : {};
  } catch {
    return {};
  }
}

// The byte count the server would have recorded — measured ENCODED, not by
// `String.length`, which counts UTF-16 code units and would under-report any
// non-ASCII spec file.
function byteLength(content: string): number {
  return new TextEncoder().encode(content).byteLength;
}

export function recordAppliedFiles(
  projectName: string,
  writes: { path: string; content: string }[],
): FileMeta[] {
  const all = loadAppliedFiles();
  const files = all[projectName] ?? [];
  const applied = writes.map((w) => ({
    path: w.path,
    content: w.content,
    size: byteLength(w.content),
    sha: mockSha(w.path + w.content),
  }));
  for (const file of applied) {
    const existing = files.findIndex((f) => f.path === file.path);
    if (existing >= 0) files[existing] = file;
    else files.push(file);
  }
  all[projectName] = files;
  try {
    localStorage.setItem(APPLIED_FILES_KEY, JSON.stringify(all));
  } catch {
    /* quota — non-fatal in mock mode */
  }
  return applied.map(({ path, sha, size }) => ({ path, sha, size }));
}

export function appliedFileMetas(projectName: string): FileMeta[] {
  return (loadAppliedFiles()[projectName] ?? []).map((f) => ({
    path: f.path,
    sha: f.sha,
    size: f.size,
  }));
}

export function appliedFileContent(
  projectName: string,
  path: string,
): FileContent | null {
  const file = (loadAppliedFiles()[projectName] ?? []).find(
    (f) => f.path === path,
  );
  if (!file) return null;
  return { path: file.path, content: file.content, sha: file.sha };
}

export const applyFilesError: ApiError = {
  code: "internal_error",
  message: "Mock error scenario for files/apply",
};

// The create flow's reference upload (#383) failing — the surface behind the
// confirm step's Retry / Continue-without-documents pair.
export const uploadReferencesError: ApiError = {
  code: "internal_error",
  message: "Mock error scenario for the reference upload",
};

export const projectSectionError: ApiError = {
  code: "internal_error",
  message: "Mock error scenario for the project overview",
};
