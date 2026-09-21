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
 * ONE PROCESS, nine steps, and it takes everything down when it ends.
 *
 *   1  preflight — docker, compose, openssl, a Dockerfile per service
 *   2  plan      — design.json + security.json → WirePlan, shown and confirmed
 *   3  mint      — the gateway keypair and one mock bearer per role
 *   4  compose   — the file, then `up --build --wait`
 *   5  webapp    — `npm ci` if the tree came from the runner image, then dev:mock
 *   6  pick      — who are you entering as
 *   7  open      — the browser, unless there is no terminal to have asked
 *   8  seed      — optional, replayed from a script after the first time
 *   9  panel     — keys while you test; q tears it all down
 *
 * There is no `up`, `down`, `status` or `restart` verb. The process lifetime IS
 * the session: quitting is the cleanup, and a session that died hard is reaped
 * by the next start, by compose project name. That is one thing to learn and
 * one thing that can be left running by mistake, rather than four.
 */

import { appendFileSync, existsSync, readdirSync, statSync } from "node:fs";
import { stdout as output } from "node:process";
import * as clack from "@clack/prompts";
import { projectSlug } from "../../ports/spec-workspace.js";
import { loadProjectState, saveProjectState } from "../../state/project.js";
import { openPinnedPane } from "../pinned-pane.js";
import { ensureKeypair, mintAssertion, roleTokens, WIRE_HEADER, WIRE_ISSUER } from "./assertion.js";
import type { WireSession } from "./state.js";
import { composeDown, composeLogs, composePs, composeUp, composeUpOne, type ComposeTarget } from "./docker.js";
import { panelRows, readyLine, resolveKey, type PanelModel } from "./panel.js";
import {
  assignHostPorts,
  buildWirePlan,
  describePlan,
  planBlockers,
  readWireSpecs,
  type WirePlan,
  type WireService,
} from "./plan.js";
import { composeDocument, maskedPlan } from "./compose.js";
import { entryUrl, findEntry, roleEntries, type RoleEntry } from "./roles.js";
import { answers, isPortAvailable, isPortBusy, killListener, openBrowser } from "./runtime.js";
import {
  databaseSecret,
  ensureWireDir,
  readWireSession,
  sessionProcessExists,
  wirePaths,
  writeFileAtomic,
  writeWireSession,
} from "./state.js";
import { installIfNeeded, needsInstall, startWiredDevServer, type DevServer } from "./webapp.js";
import { runSeedAgent } from "./agents/seed.js";
import { runTriageAgent } from "./agents/triage.js";

export interface WireOptions {
  /** `--role <name>`: skip the picker. "" is the no-grant caller. */
  role?: string;
  /** `--seed`: run the seed step once everything answers. */
  seed?: boolean;
  /** `--fresh`: drop the database volume before starting. */
  fresh?: boolean;
  /** `--no-open`: print `READY <url>` instead of opening a browser. */
  noOpen?: boolean;
  /** `--yes`: skip the first-run confirmation. */
  yes?: boolean;
  /** `--skip <dep>`: start anyway with that dependency's env unset. */
  skip?: string[];
  /** `--no-triage`: do not ask a model to read the logs when a service fails. */
  noTriage?: boolean;
  /** Quiet: tests. */
  silent?: boolean;
}

export interface WireOutcome {
  ok: boolean;
  detail?: string;
}

/** Everything a running session has to be able to take down again. */
interface Running {
  target: ComposeTarget;
  dev: DevServer | null;
}

export async function wireCommand(
  projectDir: string,
  options: WireOptions = {},
  confirmDir?: () => Promise<boolean>,
): Promise<WireOutcome> {
  const say = (line: string): void => {
    if (!options.silent) output.write(`${line}\n`);
  };

  // --- 1 preflight ----------------------------------------------------------
  const missing = await missingTools();
  if (missing.length > 0) return { ok: false, detail: `not available: ${missing.join(", ")}` };

  const previous = readWireSession(projectDir);
  // Both halves: the process exists AND its dev server is still answering. The
  // pid alone would refuse forever behind a zombie; the port alone would refuse
  // because some other program took 5173.
  const stillRunning =
    sessionProcessExists(previous) && (previous?.webappPort ? await isPortBusy(previous.webappPort) : true);
  if (stillRunning) {
    return {
      ok: false,
      detail:
        `a wired session is already running for this project (${previous?.composeProject ?? ""}, pid ${String(previous?.pid ?? 0)})` +
        `${previous?.webappPort ? ` on http://localhost:${String(previous.webappPort)}` : ""} — quit it with q in its terminal first`,
    };
  }

  // --- 2 plan ---------------------------------------------------------------
  const slug = projectSlug(projectDir);
  const plan = buildWirePlan(readWireSpecs(projectDir, slug), {
    secret: (database) => databaseSecret(projectDir, database),
  });
  await assignHostPorts(plan, isPortAvailable);

  const blockers = planBlockers(plan, projectDir, options.skip ?? []);
  if (blockers.length > 0) {
    for (const blocker of blockers) say(`  ✗ ${blocker}`);
    return { ok: false, detail: "the plan does not hold" };
  }
  if (plan.services.length === 0 && plan.webapp) {
    // Nothing to wire: the app has no sibling service, so mock mode already is
    // the whole story and standing a compose project up would add nothing.
    say(`  this project has no service to wire — ${plan.webapp.appPath}: npm run dev:mock is the whole story`);
    return { ok: true };
  }
  say("");
  for (const line of describePlan(plan)) say(line);
  say("");

  const state = loadProjectState(projectDir, slug);
  if (!state.wireConfirmed && !options.yes) {
    if (!confirmDir || !(await confirmDir())) {
      return { ok: false, detail: "not confirmed — re-run with --yes or confirm in the TUI" };
    }
    state.wireConfirmed = true;
    saveProjectState(projectDir, state);
  }

  // --- 3 mint ---------------------------------------------------------------
  const paths = wirePaths(projectDir);
  ensureWireDir(projectDir);
  const keypair = ensureKeypair(paths.dir);
  const tokens = roleTokens(plan.roles);
  writeFileAtomic(paths.tokens, JSON.stringify(tokens, null, 2));

  // --- 4 compose ------------------------------------------------------------
  writeFileAtomic(
    paths.compose,
    composeDocument(
      plan,
      { ...keypair, issuer: WIRE_ISSUER, header: WIRE_HEADER },
      projectDir,
      existsSync(paths.initSql) ? paths.initSql : undefined,
    ),
  );
  writeFileAtomic(paths.plan, JSON.stringify(maskedPlan(plan), null, 2));
  const target: ComposeTarget = { file: paths.compose, project: plan.composeProject };

  for (const warning of staleVolumeWarnings(projectDir, plan, previous)) say(`  ⚠ ${warning}`);
  await reapStaleSession(previous, target, say);
  if (options.fresh) {
    say("  ↺ --fresh: dropping the database volume");
    await composeDown(target, true);
  }

  say(`  building and starting ${plan.composeProject} (a cold image build is minutes, not seconds)`);
  const up = await composeUp(target, (line) => {
    appendFileSync(logFile(projectDir, "compose"), `${line}\n`);
    if (!options.silent && /error|Error|ERROR|exited|unhealthy/.test(line)) output.write(`    ${line}\n`);
  });
  if (up.code !== 0) {
    await reportBringUpFailure(projectDir, target, plan, up.output, options, say);
    await composeDown(target);
    return { ok: false, detail: "the backend did not come up" };
  }
  writeWireSession(projectDir, {
    composeProject: plan.composeProject,
    startedAt: new Date().toISOString(),
    pid: process.pid,
  });
  for (const service of plan.services) {
    say(`  ✓ ${service.name} — http://localhost:${String(service.hostPort)}`);
  }

  const running: Running = { target, dev: null };
  // ONE teardown, shared as a promise rather than guarded by a flag. Two paths
  // reach it — a signal, and the end of the session — and with a boolean guard
  // the second returns instantly while the first is still waiting on `compose
  // down`, so the process exits with the containers still up. Measured exactly
  // that way once: the dev server was reaped and the compose project was not.
  let tearing: Promise<void> | null = null;
  const teardown = (): Promise<void> => {
    tearing ??= (async () => {
      if (running.dev) await running.dev.group.stop();
      await composeDown(running.target);
      if (running.dev && (await isPortBusy(running.dev.port))) await killListener(running.dev.port);
      say("STOPPED");
    })();
    return tearing;
  };
  const onSignal = (): void => {
    void teardown().then(() => {
      process.exit(0);
    });
  };
  process.on("SIGINT", onSignal);
  process.on("SIGTERM", onSignal);

  try {
    // --- 5 webapp -----------------------------------------------------------
    const primary = plan.services[0];
    if (plan.webapp && primary) {
      const appPath = `${projectDir}/${plan.webapp.appPath}`;
      const apiService = plan.services.find((service) => service.name === plan.webapp?.apiService) ?? primary;
      if (needsInstall(appPath)) {
        say("  installing the app's dependencies on this host (the coding run installed them inside the image)");
        await installIfNeeded(appPath, (line) => {
          appendFileSync(logFile(projectDir, "webapp"), `${line}\n`);
        });
      }
      running.dev = await startWiredDevServer(
        appPath,
        {
          api: `http://localhost:${String(apiService.hostPort)}`,
          keyPath: keypair.keyPath,
          issuer: WIRE_ISSUER,
          header: WIRE_HEADER,
        },
        (line) => {
          appendFileSync(logFile(projectDir, "webapp"), `${line}\n`);
        },
      );
      writeWireSession(projectDir, {
        composeProject: plan.composeProject,
        webappPort: running.dev.port,
        startedAt: new Date().toISOString(),
        pid: process.pid,
      });
    }

    // --- 6 pick, 7 open -----------------------------------------------------
    const entries = roleEntries(plan.roles);
    const interactive = process.stdin.isTTY === true && !options.silent;
    let entry: RoleEntry | null = null;
    if (options.role !== undefined) {
      entry = findEntry(entries, options.role);
      if (!entry) {
        await teardown();
        return { ok: false, detail: `no role called "${options.role}" — ${entries.map((e) => e.label).join(", ")}` };
      }
    } else if (interactive && running.dev) {
      entry = await pickRole(entries);
      if (!entry) {
        await teardown();
        return { ok: true };
      }
    }

    let url: string | null = null;
    if (running.dev) {
      url = entry ? entryUrl(running.dev.url, entry) : running.dev.url;
      say(readyLine(url));
      if (!options.noOpen && interactive) await openBrowser(url);
    } else {
      // No web application: the person IS the client, so the session hands them
      // one call per role instead of a page.
      for (const line of await curlTable(plan, keypair.keyPath)) say(line);
    }

    // --- 8 seed -------------------------------------------------------------
    if (options.seed && primary) {
      // The dev server's base URL, not the role URL that was just opened: the
      // seed script carries its own bearer per call and a query string in the
      // base would ride into every one of them.
      const seedBase = running.dev?.url ?? `http://localhost:${String(primary.hostPort)}`;
      const seeded = await seed(projectDir, plan, seedBase, running.dev !== null);
      say(`  ${seeded}`);
    }

    // --- 9 panel ------------------------------------------------------------
    if (interactive) {
      await panelLoop(projectDir, plan, running, entries, url);
    } else if (!options.role && !options.silent) {
      // A script asked for this without naming a role and has nothing to hold
      // the session open with; saying so beats exiting as if it had worked.
      say("  (not a terminal: pass --role to hold the session open for a driver)");
    } else {
      await waitForTermination();
    }
  } finally {
    process.off("SIGINT", onSignal);
    process.off("SIGTERM", onSignal);
    await teardown();
  }
  return { ok: true };
}

// --- steps ------------------------------------------------------------------

/** Everything the session shells out to, checked before anything is written. */
async function missingTools(): Promise<string[]> {
  const checks: [string, Promise<boolean>][] = [
    ["docker (is the daemon running?)", answers("docker", ["info"])],
    ["docker compose", answers("docker", ["compose", "version"])],
    ["openssl", answers("openssl", ["version"])],
  ];
  const missing: string[] = [];
  for (const [name, check] of checks) if (!(await check)) missing.push(name);
  return missing;
}

/**
 * Reap what a hard-killed session left running.
 *
 * By compose project name and by the port in `session.json`, because those are
 * the two handles that survive a process: SIGKILL, a closed terminal or a
 * laptop lid leave a compose project up and a dev server listening, and the
 * next start would otherwise fail on a port or, worse, talk to yesterday's
 * containers.
 */
async function reapStaleSession(
  previous: WireSession | null,
  target: ComposeTarget,
  say: (line: string) => void,
): Promise<void> {
  await composeDown(target);
  if (previous?.webappPort && (await isPortBusy(previous.webappPort))) {
    if (await killListener(previous.webappPort)) {
      say(`  ↩ reaped a dev server left on port ${String(previous.webappPort)}`);
    }
  }
}

/**
 * The volume outlives the code, and nothing migrates it.
 *
 * A service edited since the last session may create its schema differently
 * from the one already in the data directory, and the failure surfaces as a
 * query against a column that is not there — which reads as an application bug.
 * Saying it once, before the build, is the difference between that and `--fresh`.
 */
function staleVolumeWarnings(projectDir: string, plan: WirePlan, previous: WireSession | null): string[] {
  if (!previous || plan.databases.length === 0) return [];
  const lastStart = Date.parse(previous.startedAt);
  if (Number.isNaN(lastStart)) return [];
  return plan.services
    .filter((service) => newestFileTime(`${projectDir}/${service.appPath}`) > lastStart)
    .map(
      (service) =>
        `${service.appPath}/ has changed since this project's volume was last started — ` +
        `if its schema moved, re-run with --fresh`,
    );
}

/** The newest mtime in a tree, skipping what a build drops in it. */
function newestFileTime(dir: string, depth = 4): number {
  const skip = new Set(["node_modules", "target", "dist", ".git", "generated"]);
  let newest = 0;
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return 0;
  }
  for (const entry of entries) {
    if (entry.name.startsWith(".") || skip.has(entry.name)) continue;
    const full = `${dir}/${entry.name}`;
    if (entry.isDirectory()) {
      if (depth > 0) newest = Math.max(newest, newestFileTime(full, depth - 1));
    } else {
      try {
        newest = Math.max(newest, statSync(full).mtimeMs);
      } catch {
        // gone between the listing and the stat; nothing to compare
      }
    }
  }
  return newest;
}

function logFile(projectDir: string, name: string): string {
  return `${wirePaths(projectDir).logs}/${name}.log`;
}

/** The bring-up failed: say which service, tail its log, and offer the model a look. */
async function reportBringUpFailure(
  projectDir: string,
  target: ComposeTarget,
  plan: WirePlan,
  upOutput: string,
  options: WireOptions,
  say: (line: string) => void,
): Promise<void> {
  const rows = await composePs(target);
  const broken =
    rows.find((row) => row.state === "exited" || row.health === "unhealthy")?.name ?? plan.services[0]?.name ?? "";
  say(`  ✗ ${broken || "the backend"} did not come up`);
  const logs = broken ? await composeLogs(target, broken) : upOutput;
  for (const line of logs.trimEnd().split("\n").slice(-30)) say(`    ${line}`);

  if (options.noTriage) return;
  say("  reading the logs…");
  const verdict = await runTriageAgent({ projectDir, service: broken, logs, plan });
  say(verdict.ok ? `  ${verdict.summary}` : `  (triage unavailable: ${verdict.summary})`);
}

/** The seed step: write the script once with a model, replay it every time after. */
async function seed(projectDir: string, plan: WirePlan, proxyUrl: string, proxied: boolean): Promise<string> {
  const result = await runSeedAgent({
    projectDir,
    plan,
    proxyUrl,
    proxied,
    tokensFile: wirePaths(projectDir).tokens,
  });
  return result.summary;
}

/** One `curl` per role, for a project with no page to click. */
async function curlTable(plan: WirePlan, keyPath: string): Promise<string[]> {
  const service = plan.services[0];
  if (!service) return [];
  const lines = ["", "  No web application in this design — call the service as each role:", ""];
  for (const role of plan.roles) {
    const assertion = await mintAssertion(role, keyPath);
    lines.push(`  # ${role.name} (${role.username})`);
    lines.push(`  curl -sS -H '${WIRE_HEADER}: ${assertion}' http://localhost:${String(service.hostPort)}/`);
  }
  lines.push("");
  lines.push("  The scope check the gateway would do is NOT in front of these — an assertion is identity, not permission.");
  return lines;
}

/** Hold the session open for a driver that has no keyboard: until it is killed. */
function waitForTermination(): Promise<void> {
  return new Promise((resolve) => {
    process.once("SIGINT", () => {
      resolve();
    });
    process.once("SIGTERM", () => {
      resolve();
    });
  });
}

// --- the terminal -----------------------------------------------------------

async function pickRole(entries: RoleEntry[]): Promise<RoleEntry | null> {
  const picked = await clack.select<string>({
    message: "Enter the app as",
    options: entries.map((entry, index) => ({
      value: String(index),
      label: entry.label,
      hint: entry.hint,
    })),
  });
  if (clack.isCancel(picked)) return null;
  return entries[Number(picked)] ?? null;
}

/**
 * The panel, until `q`.
 *
 * Raw mode, because one keystroke should do one thing — and because a session
 * whose only affordance was a prompt would make the terminal a modal thing while
 * the browser is where the work is happening. Raw mode also means Ctrl-C no
 * longer becomes a signal, so `resolveKey` treats it as quit.
 */
async function panelLoop(
  projectDir: string,
  plan: WirePlan,
  running: Running,
  entries: RoleEntry[],
  url: string | null,
): Promise<void> {
  const pane = openPinnedPane(output, output.isTTY === true);
  const model: PanelModel = {
    url,
    services: await composePs(running.target),
    webapp: running.dev ? { url: running.dev.url, port: running.dev.port } : null,
  };
  const repaint = (): void => {
    pane.set(panelRows(model, entries, pane.width()));
  };
  repaint();

  const stdin = process.stdin;
  const wasRaw = stdin.isRaw === true;
  stdin.setRawMode(true);
  stdin.resume();
  stdin.setEncoding("utf8");

  try {
    for (;;) {
      const key = await nextKey(stdin);
      const action = resolveKey(key, entries);
      if (action.kind === "quit") return;
      if (action.kind === "none") continue;

      if (action.kind === "role") {
        if (!running.dev) {
          model.note = "no web application in this project — the roles are in the curl table above";
          repaint();
          continue;
        }
        model.url = entryUrl(running.dev.url, action.entry);
        model.note = `opened as ${action.entry.label}`;
        repaint();
        await openBrowser(model.url);
        continue;
      }

      // Everything below borrows the terminal back: a prompt cannot read a
      // keyboard the panel is holding in raw mode.
      stdin.setRawMode(false);
      stdin.pause();
      try {
        if (action.kind === "rebuild") {
          const service = await pickService(plan.services);
          if (service) {
            pane.line(`  rebuilding ${service.name}…`);
            const result = await composeUpOne(running.target, service.name, (line) => {
              appendFileSync(logFile(projectDir, "compose"), `${line}\n`);
            });
            model.note = result.code === 0 ? `${service.name} rebuilt and restarted` : `${service.name} failed to rebuild — see logs`;
          }
        } else if (action.kind === "logs") {
          const service = await pickService(plan.services);
          if (service) {
            const logs = await composeLogs(running.target, service.name, 40);
            for (const line of logs.trimEnd().split("\n")) pane.line(`    ${line}`);
            model.note = `last 40 lines of ${service.name}`;
          }
        } else {
          pane.line("  seeding…");
          model.note = await seed(
            projectDir,
            plan,
            running.dev?.url ?? `http://localhost:${String(plan.services[0]?.hostPort ?? 0)}`,
            running.dev !== null,
          );
        }
      } finally {
        stdin.setRawMode(true);
        stdin.resume();
      }
      model.services = await composePs(running.target);
      repaint();
    }
  } finally {
    stdin.setRawMode(wasRaw);
    stdin.pause();
    pane.close();
  }
}

function nextKey(stdin: NodeJS.ReadStream): Promise<string> {
  return new Promise((resolve) => {
    stdin.once("data", (chunk: string | Buffer) => {
      resolve(typeof chunk === "string" ? chunk : chunk.toString("utf8"));
    });
  });
}

async function pickService(services: WireService[]): Promise<WireService | null> {
  if (services.length === 0) return null;
  if (services.length === 1) return services[0] ?? null;
  const picked = await clack.select<string>({
    message: "Which service",
    options: services.map((service) => ({ value: service.name, label: service.name })),
  });
  if (clack.isCancel(picked)) return null;
  return services.find((service) => service.name === picked) ?? null;
}
