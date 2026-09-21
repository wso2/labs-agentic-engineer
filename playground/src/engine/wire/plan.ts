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

/**
 * WHAT `wire` WILL STAND UP, worked out before anything starts.
 *
 * A pure function of the project bundle: every component's `design.json`, its
 * shipped `workload.yaml`, and the project's `security.json` in, a `WirePlan`
 * out. No AI reads these files and none is asked to guess — the env var a
 * Postgres host lands in is written in that component's `workload.yaml`, the
 * same file the platform projects a deployed pod's environment from, and a
 * value that can be READ is never invented.
 *
 * A dependency that binds NOTHING is unresolved, never silently fine. The
 * difference is a service that refuses to plan versus one that starts, passes
 * its health check, and 500s every query against `localhost:5432`.
 *
 * What the plan deliberately does NOT decide: whether anything is running,
 * whether a port is free, or what the keypair is. Those are session facts, so
 * they arrive as arguments (`assignHostPorts`) or live in `session.ts`. A plan
 * that reads the world cannot be diffed, golden-tested, or shown to a developer
 * before they agree to it.
 */

import { existsSync, readFileSync } from "node:fs";
import { randomBytes } from "node:crypto";
import { join } from "node:path";
import { parse } from "yaml";
import type { ComponentDesign, Dependency } from "@aep/agent-stream";
import { listComponents } from "../gates.js";

/** The first host port a service is mapped to; the next takes the one after. */
export const FIRST_HOST_PORT = 19090;

/**
 * The port every service listens on inside its container — the component
 * contract's, unchanged. Siblings reach each other by service name on this
 * port, so two services never collide and no stack skill learns a new variable.
 */
export const SERVICE_PORT = 9090;

/** One service to build and run: a `type: "service"` component. */
export interface WireService {
  name: string;
  /** Project-relative source dir — compose's build context. */
  appPath: string;
  /** The host port compose maps `SERVICE_PORT` to. Assigned by `assignHostPorts`. */
  hostPort: number;
  /** Everything the container gets besides the gateway trio, which `compose.ts` adds. */
  env: Record<string, string>;
  /**
   * The keys of `env` that hold a secret, named here because THIS is where it is
   * known: a value is a secret because of the dependency output it came from,
   * never because of the variable it landed in. The design names that variable,
   * so `DB_PASS` or a lowercase `db_password` is entirely legal and reads as
   * nothing in particular to a pattern — and the thing on the other side of the
   * guess is a password written into `plan.json` and put in an agent prompt.
   */
  secretEnv: string[];
  /** Sibling services and databases that must be healthy first. */
  dependsOn: string[];
}

/** One `postgres-cnpg` dependency, as a container. */
export interface WireDatabase {
  /** The dependency's name — also the compose service name siblings reach it by. */
  name: string;
  database: string;
  user: string;
  password: string;
  /** The named volume the data lives in, so a session's rows survive to the next. */
  volume: string;
}

/** The single-page app, which runs on the host under Vite rather than in compose. */
export interface WireWebapp {
  name: string;
  appPath: string;
  /** The sibling service its `/api` is proxied to, when it declares one. */
  apiService: string | null;
}

/** One role a session can be entered as. */
export interface WireRole {
  name: string;
  grants: string[];
  /** The `testUsers` row, which becomes the assertion's `username`; the role name when there is none. */
  username: string;
  /** True when no `testUsers` row named it — `/me/` reach will find nothing. */
  usernameGuessed: boolean;
}

/** A dependency nothing here can stand in for. */
export interface WireUnresolved {
  component: string;
  dependency: string;
  kind: string;
  resourceType?: string;
}

export interface WirePlan {
  slug: string;
  /** `docker compose -p <this>` — what a crashed session is reaped by. */
  composeProject: string;
  services: WireService[];
  databases: WireDatabase[];
  webapp: WireWebapp | null;
  roles: WireRole[];
  unresolved: WireUnresolved[];
  /** Things worth saying out loud that do not stop the run. */
  warnings: string[];
}

/** The design bundle, read off disk by `readWireSpecs`. */
export interface WireSpecs {
  slug: string;
  designs: ComponentDesign[];
  security: SecurityDesign;
  /**
   * Each component's shipped `workload.yaml` bindings, by component name.
   * `undefined` for a component that has none on disk — which means it was
   * never built, and is reported as such rather than read as "needs nothing".
   */
  workloads: Record<string, WorkloadBindings | undefined>;
}

interface SecurityDesign {
  roles?: { name?: unknown; grants?: unknown }[];
  testUsers?: { username?: unknown; roles?: unknown }[];
}

export interface PlanOptions {
  /** The password for a database, by name. Injected because it must be stable — see `databaseSecret`. */
  secret?: (database: string) => string;
}

/**
 * The `resourceType`s wired mode stands up itself, and therefore the ones whose
 * bindings it must find. Anything else is already reported as unresolved by the
 * dependency walk below, so it needs no second check.
 */
const STANDS_UP = new Set(["postgres-cnpg", "thunder-app"]);

/**
 * The dependency OUTPUTS whose value is a secret, whatever variable a design
 * binds them to. The one list `secretEnv` is built from — see `WireService`.
 */
const SECRET_OUTPUTS = new Set(["password", "client_secret"]);

/** `onboarding-db` → `ONBOARDING_DB`, the shape `envBindings` keys are prefixed with. */
function envPrefix(name: string): string {
  return name.toUpperCase().replace(/[^A-Z0-9]/g, "_");
}

/** A Postgres identifier from a dependency name: hyphens are not legal in one unquoted. */
function identifier(name: string): string {
  return name.replace(/[^a-zA-Z0-9_]/g, "_");
}

/** Every `envBindings` block a component's `workload.yaml` declares, by dependency name. */
export type WorkloadBindings = Record<string, Record<string, string>>;

/**
 * What a component's SHIPPED `workload.yaml` calls each dependency's values.
 *
 * This file is the authority for binding names, not `design.json`'s
 * `wiring.envBindings`. Three reasons, and the third is the one that bit:
 * `workload.yaml` is mandatory at the App Path root for every component the
 * contract accepts; it is the artifact the platform itself projects env from,
 * so reading it is what makes a wired container's environment the same shape a
 * deployed pod gets; and `wiring` is a design-time field that a design can
 * simply not carry — both projects generated on 2026-09-17 declared their
 * dependencies with no `wiring` key at all, which read as "no variables
 * needed" and started every service with an unconfigured database.
 *
 * Two shapes, because the platform models the two dependency kinds
 * differently: `resources[].ref` names a platform resource, `endpoints[]`
 * names a sibling component and prefixes it with the project slug.
 */
function readWorkloadBindings(projectDir: string, appPath: string): WorkloadBindings | undefined {
  const file = join(projectDir, appPath, "workload.yaml");
  if (!existsSync(file)) return undefined;
  let doc: unknown;
  try {
    doc = parse(readFileSync(file, "utf8"));
  } catch {
    // Unparseable is NOT "absent": absent means the component was never built,
    // while broken means somebody has to look at it. Returning undefined here
    // would blame the wrong thing, so let it read as an empty declaration and
    // let the per-dependency check below name the dependency that went unbound.
    return {};
  }
  const dependencies = (doc as { dependencies?: unknown }).dependencies;
  if (typeof dependencies !== "object" || dependencies === null) return {};
  const bindings: WorkloadBindings = {};
  const record = (name: unknown, envBindings: unknown): void => {
    if (typeof name !== "string" || typeof envBindings !== "object" || envBindings === null) return;
    const pairs = Object.entries(envBindings).filter(
      (pair): pair is [string, string] => typeof pair[1] === "string",
    );
    if (pairs.length > 0) bindings[name] = Object.fromEntries(pairs);
  };
  const { resources, endpoints } = dependencies as { resources?: unknown; endpoints?: unknown };
  for (const entry of Array.isArray(resources) ? resources : []) {
    const { ref, envBindings } = (entry ?? {}) as { ref?: unknown; envBindings?: unknown };
    record(ref, envBindings);
  }
  for (const entry of Array.isArray(endpoints) ? endpoints : []) {
    const { component, envBindings } = (entry ?? {}) as { component?: unknown; envBindings?: unknown };
    record(component, envBindings);
  }
  return bindings;
}

/**
 * This dependency's binding names, matched by the component's own workload.
 *
 * A platform resource's key is `resources[].ref`, and the design stamps that
 * exact value on the dependency as `wiring.ref` (see ResourceWiring) — so that
 * is the join, and it has to be tried FIRST. The ref is opaque: the platform
 * mints `<truncated-slug>-<hash>`, which neither equals the dependency name nor
 * ends with it, so a name-only match silently found nothing and every
 * platform-resource read as "binds no variable" — which `wire` reports as
 * "cannot stand in for", blocking any project with a database or an auth
 * dependency.
 *
 * An endpoint entry is written `<slug>-<component>` while the design names the
 * dependency bare, so a suffix match is what joins them. The bare name is tried
 * before the suffix so an exact declaration stays authoritative when both could
 * apply, and a design carrying no `wiring` at all still falls through to it.
 */
function bindingsOf(dependency: Dependency, workload: WorkloadBindings | undefined): Record<string, string> {
  if (!workload) return {};
  const wiring = dependency.wiring as { ref?: string } | undefined;
  const byRef = wiring?.ref ? workload[wiring.ref] : undefined;
  if (byRef) return byRef;
  const exact = workload[dependency.name];
  if (exact) return exact;
  const suffix = Object.keys(workload).find((key) => key.endsWith(`-${dependency.name}`));
  return suffix ? (workload[suffix] ?? {}) : {};
}

/**
 * The values a `thunder-app` binding gets.
 *
 * Placeholders, on purpose. The service reads these at start — and PANICS when
 * one is empty — but in wired mode nothing authenticates against Thunder: the
 * gateway stand-in mints the assertion, and the assertion is the only identity
 * the service trusts (ADR-0032). So they exist to let the process start, and a
 * service that reaches one of these URLs is doing something wired mode does not
 * model, which is worth finding out.
 */
function thunderPlaceholders(scopes: string[]): Record<string, string> {
  return {
    client_id: "aep-playground-wire",
    client_secret: "aep-playground-wire",
    issuer: "https://thunder.invalid/oauth2/token",
    jwks_url: "https://thunder.invalid/oauth2/jwks",
    resource: "aep-playground-wire",
    scopes: scopes.join(" "),
  };
}

/**
 * The plan. Pure: the same bundle plans the same way every time, apart from the
 * generated database passwords, which is what `secret` is for.
 */
export function buildWirePlan(specs: WireSpecs, options: PlanOptions = {}): WirePlan {
  const secret = options.secret ?? ((): string => randomBytes(12).toString("hex"));
  const plan: WirePlan = {
    slug: specs.slug,
    composeProject: `aep-wire-${specs.slug}`,
    services: [],
    databases: [],
    webapp: null,
    roles: readRoles(specs.security),
    unresolved: [],
    warnings: [],
  };
  const allGrants = [...new Set(plan.roles.flatMap((role) => role.grants))].sort();
  const databases = new Map<string, WireDatabase>();

  for (const design of specs.designs) {
    if (design.type === "web-application") {
      const api = (design.dependencies ?? []).find((dependency) => dependency.kind === "component");
      plan.webapp = { name: design.name, appPath: design.appPath, apiService: api?.name ?? null };
      // The webapp's own dependencies are not stood up: it runs under Vite and
      // reaches the API through the dev server's proxy, and its Thunder app is
      // replaced by the mock session. Nothing to resolve, nothing to report.
      continue;
    }
    if (design.type !== "service") {
      plan.warnings.push(`${design.name}: type "${design.type}" is not stood up — only services run in compose`);
      continue;
    }

    const env: Record<string, string> = {};
    const secretEnv = new Set<string>();
    const dependsOn: string[] = [];
    const workload = specs.workloads[design.name];
    for (const dependency of design.dependencies ?? []) {
      const bindings = bindingsOf(dependency, workload);
      // A dependency wire CAN stand up, that binds no variable, is unresolved.
      // Standing it up anyway is the failure this guard exists for: compose
      // starts the database, `dependsOn` orders it, the service boots against
      // its own defaults, its health check passes because nothing there touches
      // storage, and the first real query 500s. Every symptom points at the
      // generated app, and none of them points here.
      if (STANDS_UP.has(dependency.resourceType ?? "") && Object.keys(bindings).length === 0) {
        plan.unresolved.push({
          component: design.name,
          dependency: dependency.name,
          kind: dependency.kind,
          ...(dependency.resourceType ? { resourceType: dependency.resourceType } : {}),
        });
        continue;
      }
      if (dependency.kind === "platform-resource" && dependency.resourceType === "postgres-cnpg") {
        const database = databases.get(dependency.name) ?? {
          name: dependency.name,
          database: identifier(dependency.name),
          user: identifier(dependency.name),
          password: secret(dependency.name),
          volume: `${specs.slug}-${dependency.name}`,
        };
        databases.set(dependency.name, database);
        const values: Record<string, string> = {
          host: database.name,
          port: String(5432),
          dbname: database.database,
          database: database.database,
          user: database.user,
          username: database.user,
          password: database.password,
        };
        for (const [output, variable] of Object.entries(bindings)) {
          const value = values[output];
          if (value === undefined) {
            plan.warnings.push(
              `${design.name}: ${dependency.name} declares output "${output}" (${variable}) that a plain postgres container has no value for — set empty`,
            );
          }
          env[variable] = value ?? "";
          if (SECRET_OUTPUTS.has(output)) secretEnv.add(variable);
        }
        dependsOn.push(database.name);
      } else if (dependency.kind === "platform-resource" && dependency.resourceType === "thunder-app") {
        const values = thunderPlaceholders(allGrants);
        for (const [output, variable] of Object.entries(bindings)) {
          env[variable] = values[output] ?? "";
          if (SECRET_OUTPUTS.has(output)) secretEnv.add(variable);
        }
      } else if (dependency.kind === "component") {
        // A sibling is reached by its compose service name, on the one port
        // every service listens on. The gateway URL a deployed call would use
        // is deliberately left unset: there is no gateway between two
        // containers on one network, and a service that needs one here would be
        // modelling something wired mode does not have.
        const variable = bindings.address ?? `${envPrefix(dependency.name)}_URL`;
        env[variable] = `http://${dependency.name}:${String(SERVICE_PORT)}`;
        dependsOn.push(dependency.name);
      } else {
        plan.unresolved.push({
          component: design.name,
          dependency: dependency.name,
          kind: dependency.kind,
          ...(dependency.resourceType ? { resourceType: dependency.resourceType } : {}),
        });
      }
    }

    plan.services.push({
      name: design.name,
      appPath: design.appPath,
      hostPort: 0, // assigned by assignHostPorts, which is the only thing that looks at the machine
      env,
      secretEnv: [...secretEnv],
      dependsOn,
    });
  }

  plan.databases = [...databases.values()];
  return plan;
}

/**
 * Give every service a free host port, in declared order.
 *
 * Separate from `buildWirePlan` and takes its "is this port free?" answer as an
 * argument, because whether 19090 is taken is a fact about the machine at this
 * second: folding it into the plan would make the plan untestable and its
 * golden file a lie the moment anything else was listening.
 */
export async function assignHostPorts(
  plan: WirePlan,
  isFree: (port: number) => Promise<boolean>,
  from = FIRST_HOST_PORT,
): Promise<WirePlan> {
  let candidate = from;
  for (const service of plan.services) {
    while (!(await isFree(candidate))) candidate += 1;
    service.hostPort = candidate;
    candidate += 1;
  }
  return plan;
}

/** The roles a session can be entered as, with the login name each one gets. */
function readRoles(security: SecurityDesign): WireRole[] {
  const users = (security.testUsers ?? []).flatMap((user) =>
    typeof user.username === "string" && Array.isArray(user.roles)
      ? [{ username: user.username, roles: user.roles.filter((role): role is string => typeof role === "string") }]
      : [],
  );
  return (security.roles ?? []).flatMap((role) => {
    if (typeof role.name !== "string" || role.name === "") return [];
    const grants = Array.isArray(role.grants) ? role.grants.filter((g): g is string => typeof g === "string") : [];
    const username = users.find((user) => user.roles.includes(role.name as string))?.username;
    return [
      {
        name: role.name,
        grants,
        username: username ?? role.name,
        usernameGuessed: username === undefined,
      },
    ];
  });
}

/** Read the design bundle off disk. The only I/O on the way to a plan. */
export function readWireSpecs(projectDir: string, slug: string): WireSpecs {
  const designs: ComponentDesign[] = [];
  const workloads: Record<string, WorkloadBindings | undefined> = {};
  for (const name of listComponents(projectDir)) {
    const file = join(projectDir, "specs/design/components", name, "design.json");
    if (!existsSync(file)) continue;
    const design = JSON.parse(readFileSync(file, "utf8")) as ComponentDesign;
    designs.push(design);
    workloads[design.name] = readWorkloadBindings(projectDir, design.appPath);
  }
  const securityFile = join(projectDir, "specs/design/security.json");
  const security = existsSync(securityFile)
    ? (JSON.parse(readFileSync(securityFile, "utf8")) as SecurityDesign)
    : {};
  return { slug, designs, security, workloads };
}

/**
 * The app has to carry the wired half of mock mode, or `wire` is theatre.
 *
 * An app generated before `mock/wired.ts` existed — or one whose `mock/` was
 * kept instead of re-copied — starts under `--mode mock` and answers every
 * `/api` call from MSW, with the real service running beside it and never
 * touched. The screens look right, the data is seed data, and nothing says so.
 * That is the exact failure this verb exists to catch, so it is a blocker and
 * the message is the fix.
 */
function webappBlockers(webapp: WireWebapp, projectDir: string): string[] {
  const mock = join(projectDir, webapp.appPath, "mock");
  const plugin = join(mock, "plugin.ts");
  const carriesWired =
    existsSync(join(mock, "wired.ts")) &&
    existsSync(plugin) &&
    readFileSync(plugin, "utf8").includes("AEP_WIRED_API");
  if (carriesWired) return [];
  return [
    `${webapp.appPath}: mock mode here predates wired mode, so the app would answer its own API and the ` +
      `service would go untouched. Re-copy the verbatim files from the react-webapp skill's assets ` +
      `(mock-plugin.ts, mock-browser.ts, mock-badge.ts, mock-wired.ts) — see its references/mock-mode.md.`,
  ];
}

/** Everything that would stop `wire` before it starts anything. */
export function planBlockers(plan: WirePlan, projectDir: string, skip: string[] = []): string[] {
  const blockers: string[] = [];
  if (plan.webapp) blockers.push(...webappBlockers(plan.webapp, projectDir));
  if (plan.services.length === 0 && plan.webapp === null) {
    blockers.push("no components to run — the design has no service and no web application");
  }
  for (const service of plan.services) {
    const dockerfile = join(projectDir, service.appPath, "Dockerfile");
    if (!existsSync(dockerfile)) {
      blockers.push(`${service.name}: no Dockerfile at ${service.appPath}/ — run the coding phase first`);
    }
  }
  for (const item of plan.unresolved) {
    if (skip.includes(item.dependency)) continue;
    blockers.push(
      `${item.component} depends on ${item.dependency} (${item.kind}${item.resourceType ? `/${item.resourceType}` : ""}), ` +
        `which wired mode cannot stand in for — re-run with --skip ${item.dependency} to start with that env unset`,
    );
  }
  return blockers;
}

/** The plan as a developer reads it before agreeing to it. Secrets never appear. */
export function describePlan(plan: WirePlan): string[] {
  const lines: string[] = [];
  lines.push(`  compose project  ${plan.composeProject}`);
  for (const database of plan.databases) {
    lines.push(`  database         ${database.name} — postgres:16, volume ${database.volume}`);
  }
  for (const service of plan.services) {
    const reaches = service.dependsOn.length > 0 ? ` → ${service.dependsOn.join(", ")}` : "";
    lines.push(
      `  service          ${service.name} — built from ${service.appPath}/Dockerfile, localhost:${String(service.hostPort)}${reaches}`,
    );
  }
  if (plan.webapp) {
    lines.push(`  webapp           ${plan.webapp.name} — vite dev:mock on the host, /api → ${plan.webapp.apiService ?? "(no sibling)"}`);
  }
  lines.push(`  roles            ${plan.roles.map((role) => role.name).join(", ") || "(none in security.json)"}`);
  for (const role of plan.roles.filter((r) => r.usernameGuessed)) {
    lines.push(`  ⚠ ${role.name} has no testUsers row — its /me/ calls will match nothing`);
  }
  for (const warning of plan.warnings) lines.push(`  ⚠ ${warning}`);
  for (const item of plan.unresolved) {
    lines.push(`  ✗ unresolved     ${item.component} → ${item.dependency} (${item.kind}${item.resourceType ? `/${item.resourceType}` : ""})`);
  }
  return lines;
}
