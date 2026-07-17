import type { components } from "../../generated/aep-api";
import { buildUsageByScenario, taskUsage } from "./usage";

type ProjectStatus = components["schemas"]["ProjectStatus"];
type ComponentList = components["schemas"]["ComponentList"];
type ComponentOpenAPI = components["schemas"]["ComponentOpenAPI"];
type TaskView = components["schemas"]["TaskView"];
type TagList = components["schemas"]["TagList"];
type BuildList = components["schemas"]["BuildList"];
type DeploymentList = components["schemas"]["DeploymentList"];
type FileMeta = components["schemas"]["FileMeta"];
type FileContent = components["schemas"]["FileContent"];
type ErrorModel = components["schemas"]["ErrorModel"];

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

const noSpec: SpecStage = { exists: false, version: "", dirty: false, design: false };
const idleBuild: BuildStage = {
  version: "",
  status: "idle",
  tasks: { total: 0, done: 0, failed: 0, active: 0 },
};
const noDeploy: DeployStage = {
  version: "",
  status: "none",
  components: { total: 0, ready: 0 },
  validation: "none",
};

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
    hasTasks: true,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true },
    build: {
      version: "v1",
      status: "running",
      tasks: { total: 4, done: 0, failed: 1, active: 3 },
    },
    deploy: noDeploy,
  },
  // v1 built, dev rollout in progress (1 of 3 components ready).
  deploying: {
    phase: "components",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: true,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true },
    build: {
      version: "v1",
      status: "succeeded",
      tasks: { total: 4, done: 4, failed: 0, active: 0 },
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
    phase: "components",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: true,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: true, design: true },
    build: {
      version: "v1",
      status: "succeeded",
      tasks: { total: 4, done: 4, failed: 0, active: 0 },
    },
    deploy: {
      version: "v1",
      status: "deployed",
      components: { total: 3, ready: 3 },
      validation: "completed",
      validationUrl: `${REPO_URL}/pull/42`,
    },
  },
  // v1 build done but the dev deployment failed.
  "deploy-failed": {
    phase: "components",
    repoStatus: "ready",
    repoUrl: REPO_URL,
    hasSpec: true,
    hasDesign: true,
    hasTasks: true,
    specStatus: "approved",
    designStatus: "approved",
    spec: { exists: true, version: "v1", dirty: false, design: true },
    build: {
      version: "v1",
      status: "succeeded",
      tasks: { total: 4, done: 4, failed: 0, active: 0 },
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

// Tasks backing list-tasks — the tasks page (#173); the overview no longer
// reads them (#183: counts ride on ProjectStatus.build.tasks).
function task(
  issueNumber: number,
  title: string,
  derivedStatus: string,
  component?: string,
): TaskView {
  const usage = taskUsage[issueNumber];
  return {
    issueNumber,
    title,
    derivedStatus,
    issueUrl: `${BOARD_URL}/${issueNumber}`,
    ...(component !== undefined && { component }),
    // Pending tasks (no execution yet) carry no usage — exercises the "—" cell (#245).
    ...(usage !== undefined && { usage }),
    attention: null,
    dependsOn: null,
    executions: {},
    hold: false,
    lineage: { specTag: "v1" },
  };
}

// The project's validation task — created alongside the coding tasks but NOT
// shown in the builds list (its status rides deploy.validation on the
// deployments board). Kept here so the list filter is exercised.
const validationTask: TaskView = {
  issueNumber: 20,
  title: "Validate acceptance criteria",
  derivedStatus: "in_progress",
  executorClass: "validation",
  operation: "validate",
  issueUrl: `${BOARD_URL}/20`,
  attention: null,
  dependsOn: null,
  executions: {},
  hold: false,
  lineage: { specTag: "v1" },
};

const buildingTasks: TaskView[] = [
  task(12, "Checkout flow with cart persistence", "pending", "storefront"),
  task(10, "Product catalog CRUD endpoints", "in_progress", "catalog-api"),
  task(9, "Scaffold storefront app shell", "merged", "storefront"),
  task(11, "Orders service payment integration", "failed", "orders-api"),
  validationTask,
];

const doneTasks: TaskView[] = buildingTasks.map((t) => ({
  ...t,
  derivedStatus: "deployed",
}));

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

// Builds backing list-project-builds — the builds page (#185): one entry per
// built tag, newest first, tallies mirroring projectStatuses[s].build.
const noBuilds: BuildList = { builds: [] };
const runningV1Build: BuildList = {
  builds: [
    {
      tag: "v1",
      status: "in_progress",
      tasks: { total: 4, done: 0, failed: 1, active: 3 },
      startedAt: "2026-07-10T09:12:00Z",
      // Mid-build the aggregate is the cost so far — it accrues on the poll (#245).
      usage: buildUsageByScenario.running,
    },
  ],
};
const completedV1Build: BuildList = {
  builds: [
    {
      tag: "v1",
      status: "completed",
      tasks: { total: 4, done: 4, failed: 0, active: 0 },
      startedAt: "2026-07-10T09:12:00Z",
      completedAt: "2026-07-10T10:03:00Z",
      usage: buildUsageByScenario.completed,
    },
  ],
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

export const projectTags: Record<
  Exclude<ProjectScenario, "error">,
  TagList
> = {
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

// The acceptance oracle authored by the validation-criteria skill — rendered
// by the read-only "Validation Criteria" view. Mixes the three methods and a
// runner-written `covered` flag (e2e only) so the view exercises every arm.
const validationCriteriaJson = JSON.stringify(
  {
    requirements: [
      {
        id: "REQ-001",
        statement:
          "Shoppers can browse and search the catalog by name and category.",
        criteria: [
          {
            id: "AC-001-a",
            must: "A shopper can search products by name and see matching results",
            method: "e2e",
            covered: true,
          },
          {
            id: "AC-001-b",
            must: "A shopper can filter the catalog by category",
            method: "e2e",
            covered: false,
          },
        ],
      },
      {
        id: "REQ-002",
        statement: "Cart contents persist across browser sessions.",
        criteria: [
          {
            id: "AC-002-a",
            must: "A cart's contents survive a browser restart for the same shopper",
            method: "e2e",
            covered: false,
          },
          {
            id: "AC-002-b",
            must: "The cart total updates promptly as items are added or removed",
            method: "scenario",
          },
        ],
      },
      {
        id: "REQ-003",
        statement:
          "Checkout produces an order visible in the shopper's order history.",
        criteria: [
          {
            id: "AC-003-a",
            must: "Completing checkout creates an order visible in order history",
            method: "e2e",
            covered: false,
          },
          {
            id: "AC-003-b",
            must: "Payment details are transmitted over an encrypted connection",
            method: "manual",
          },
        ],
      },
    ],
  },
  null,
  2,
);

// Spec files as the Files API serves them (#113): repo-relative paths under
// specs/, metadata (list-files) split from content (read-file).
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
    content: validationCriteriaJson,
  },
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
      size: f.content.length,
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

export const specFileNotFound = (path: string): ErrorModel => ({
  type: "about:blank",
  status: 404,
  title: "Not Found",
  detail: `no spec file at ${path}`,
});

export const projectSectionError: ErrorModel = {
  type: "about:blank",
  status: 500,
  title: "Internal Server Error",
  detail: "Mock error scenario for the project overview",
};
