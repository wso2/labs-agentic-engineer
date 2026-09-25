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

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { mkdtempSync, rmSync, writeFileSync, readFileSync, existsSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, isAbsolute } from "node:path";
import {
  runCli,
  resolveGraderModel,
  buildChildEnv,
  resolveBootTimeouts,
  type SpawnPromptfoo,
  type SpawnResult,
} from "../src/cli.js";

const SCENARIOS = {
  version: 1,
  component: "trip-agent",
  scenarios: [
    {
      id: "SC-001",
      brief: { goal: "Book a hotel.", facts: {}, withholds: [] },
      rubric: {
        mustCover: [{ id: "MC-1", must: "asks for dates", weight: 1 }],
        mustNot: [],
      },
    },
  ],
};

// A real promptfoo `out.json` shape, scoring SC-001 below threshold — used
// by fake spawns that want to simulate a genuine, successfully-graded run.
const FAILING_OUT_JSON = {
  results: {
    results: [
      {
        metadata: { scenarioId: "SC-001" },
        gradingResult: {
          componentResults: [{ pass: false, assertion: { metric: "MC-1" }, reason: "never asked" }],
        },
      },
    ],
  },
};

let dir: string;

function argv(
  overrides: Partial<{ scenarios: string; app: string; out: string; afm: string }> = {},
): string[] {
  const out: string[] = [];
  const push = (flag: string, value: string | undefined) => {
    if (value !== undefined) out.push(`--${flag}`, value);
  };
  push("scenarios", overrides.scenarios ?? join(dir, "scenarios.json"));
  push("app", overrides.app ?? join(dir, "app"));
  push("out", overrides.out ?? join(dir, "out"));
  push("afm", overrides.afm ?? afmPath());
  return out;
}

function afmPath(): string {
  return join(dir, "specs", "design", "components", "trip-agent", "agent.afm.md");
}

/** The agent document, laid out where `specs/design/components/` puts it. */
function writeAgentDoc(frontMatter: string): void {
  const components = join(dir, "specs", "design", "components");
  mkdirSync(join(components, "trip-agent"), { recursive: true });
  mkdirSync(join(components, "hotel-api"), { recursive: true });
  writeFileSync(
    join(components, "hotel-api", "openapi.yaml"),
    'openapi: 3.0.3\ninfo: { title: hotel-api, version: "1.0.0" }\npaths: {}\n',
  );
  writeFileSync(afmPath(), `---\n${frontMatter}---\n\n# Role\nBook hotels.\n`);
}

const AFM = `name: "trip-agent"
x-aep:
  tools:
    openapi:
      - component: "hotel-api"
        baseUrl: "\${env:HOTEL_API_URL}"
        allow: [listHotels]
`;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "agent-eval-cli-"));
  mkdirSync(join(dir, "app"), { recursive: true });
  writeFileSync(join(dir, "scenarios.json"), JSON.stringify(SCENARIOS));
  writeAgentDoc(AFM);
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe("resolveGraderModel", () => {
  it("defaults when unset", () => {
    expect(resolveGraderModel({})).toBe("anthropic:messages:claude-sonnet-5");
  });

  // A bare `env.AGENT_EVAL_GRADER ?? default` would let a set-but-blank env
  // var through as `""`, which `buildPromptfooConfig` rejects outright.
  it("defaults when set but blank", () => {
    expect(resolveGraderModel({ AGENT_EVAL_GRADER: "   " })).toBe("anthropic:messages:claude-sonnet-5");
  });

  it("uses the env value when genuinely set", () => {
    expect(resolveGraderModel({ AGENT_EVAL_GRADER: "openai:gpt-4o" })).toBe("openai:gpt-4o");
  });
});

describe("resolveBootTimeouts", () => {
  it("defaults to a short diagnosis and a longer settled bound", () => {
    expect(resolveBootTimeouts({})).toEqual({ firstBootTimeoutMs: 20_000, readyTimeoutMs: 60_000 });
  });

  it("takes an override, and keeps the settled bound above it", () => {
    expect(resolveBootTimeouts({ AGENT_EVAL_BOOT_TIMEOUT_MS: "5000" })).toEqual({
      firstBootTimeoutMs: 5_000,
      readyTimeoutMs: 60_000,
    });
  });

  // A typo must not silently become NaN, which `bootAgent` would treat as an
  // already-expired deadline and fail every boot instantly.
  it("ignores a value that is not a positive number", () => {
    expect(resolveBootTimeouts({ AGENT_EVAL_BOOT_TIMEOUT_MS: "soon" }).firstBootTimeoutMs).toBe(20_000);
    expect(resolveBootTimeouts({ AGENT_EVAL_BOOT_TIMEOUT_MS: "0" }).firstBootTimeoutMs).toBe(20_000);
    expect(resolveBootTimeouts({ AGENT_EVAL_BOOT_TIMEOUT_MS: "" }).firstBootTimeoutMs).toBe(20_000);
  });
});

describe("buildChildEnv", () => {
  it("forwards only the allowlisted keys plus the promptfoo disable flags", () => {
    const env = buildChildEnv({
      PATH: "/usr/bin",
      HOME: "/home/x",
      ANTHROPIC_API_KEY: "sk-ant-real",
      // Must never reach the child: not on the allowlist.
      CLAUDE_CODE_OAUTH_TOKEN: "coding-token",
      SOME_OTHER_SECRET: "nope",
    });
    expect(env.PATH).toBe("/usr/bin");
    expect(env.HOME).toBe("/home/x");
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-real");
    expect(env.CLAUDE_CODE_OAUTH_TOKEN).toBeUndefined();
    expect(env.SOME_OTHER_SECRET).toBeUndefined();
    expect(env.PROMPTFOO_DISABLE_TELEMETRY).toBe("1");
    expect(env.PROMPTFOO_DISABLE_UPDATE).toBe("1");
    expect(env.PROMPTFOO_DISABLE_SHARING).toBe("1");
    // The agent under test reads the org's key under its own name; the
    // judge reads it under promptfoo's. One credential, two names.
    expect(env.MODEL_API_KEY).toBe("sk-ant-real");
  });

  // In a build pod the org's key arrives as AEP_EVAL_ANTHROPIC_API_KEY, not as
  // ANTHROPIC_API_KEY: that name already belongs to Claude Code, which ranks it
  // above CLAUDE_CODE_OAUTH_TOKEN, so the platform cannot put the evaluation key
  // there without moving an OAuth-billing org's whole coding session onto it.
  it("prefers the build's evaluation key over ANTHROPIC_API_KEY", () => {
    const env = buildChildEnv({
      AEP_EVAL_ANTHROPIC_API_KEY: "sk-ant-eval",
      ANTHROPIC_API_KEY: "sk-ant-other",
      CLAUDE_CODE_OAUTH_TOKEN: "coding-token",
    });
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-eval");
    expect(env.MODEL_API_KEY).toBe("sk-ant-eval");
    expect(env.AEP_EVAL_ANTHROPIC_API_KEY).toBeUndefined();
    expect(env.CLAUDE_CODE_OAUTH_TOKEN).toBeUndefined();
  });

  // The pod's ANTHROPIC_API_KEY is the CODING credential, which an org may have
  // ring-fenced for coding and nothing else. An org whose default key is gone
  // but whose coding override is live dispatches with no evaluation key and a
  // coding key sitting under the name the fallback reads — so on a pod there is
  // no fallback at all, and the run reports that it could not evaluate.
  it("does not fall back to the pod's coding credential", () => {
    const env = buildChildEnv({
      AEP_EVAL_KEY_MANAGED: "1",
      ANTHROPIC_API_KEY: "sk-ant-the-orgs-coding-key",
    });
    expect("MODEL_API_KEY" in env).toBe(false);
    expect("ANTHROPIC_API_KEY" in env).toBe(false);
  });

  // The declaration says who OWNS the credential, not whether there is one.
  it("still uses the evaluation key the platform did mount", () => {
    const env = buildChildEnv({
      AEP_EVAL_KEY_MANAGED: "1",
      AEP_EVAL_ANTHROPIC_API_KEY: "sk-ant-eval",
      ANTHROPIC_API_KEY: "sk-ant-the-orgs-coding-key",
    });
    expect(env.MODEL_API_KEY).toBe("sk-ant-eval");
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-eval");
  });

  // ESO can materialise an empty secret. "" is no key, not a key that fails to
  // authenticate — the difference decides whether the report reads "never became
  // ready" or sends the judge at an endpoint with a blank credential.
  it("treats an empty key as no key", () => {
    expect("MODEL_API_KEY" in buildChildEnv({ ANTHROPIC_API_KEY: "" })).toBe(false);
    const env = buildChildEnv({ AEP_EVAL_ANTHROPIC_API_KEY: "", ANTHROPIC_API_KEY: "sk-ant-local" });
    expect(env.MODEL_API_KEY).toBe("sk-ant-local");
  });

  // Outside a build pod — a developer running the harness in the monorepo —
  // ANTHROPIC_API_KEY is the only key there is.
  it("falls back to ANTHROPIC_API_KEY when no evaluation key is set", () => {
    const env = buildChildEnv({ ANTHROPIC_API_KEY: "sk-ant-local" });
    expect(env.ANTHROPIC_API_KEY).toBe("sk-ant-local");
    expect(env.MODEL_API_KEY).toBe("sk-ant-local");
  });

  // The coding agent's OAuth token is not a model credential and never stands in
  // for one: it is the platform's own coding budget, and it authenticates none
  // of the API calls the judge makes.
  it("never falls back to the coding agent's OAuth token", () => {
    const env = buildChildEnv({ CLAUDE_CODE_OAUTH_TOKEN: "coding-token" });
    expect("MODEL_API_KEY" in env).toBe(false);
    expect("ANTHROPIC_API_KEY" in env).toBe(false);
  });

  it("never invents a MODEL_API_KEY when the org key is unset", () => {
    expect("MODEL_API_KEY" in buildChildEnv({})).toBe(false);
  });

  it("omits an allowlisted key that was never set, rather than forwarding undefined", () => {
    const env = buildChildEnv({});
    expect("ANTHROPIC_API_KEY" in env).toBe(false);
  });
});

function ok(status = 0): SpawnResult {
  return { status, stderr: "" };
}

describe("runCli", () => {
  it("renders a genuine scored report and never throws for a failing verdict — pins mutation #4 (exit-0-on-failing-verdict)", () => {
    const spawn: SpawnPromptfoo = (args) => {
      const outPath = args[args.indexOf("-o") + 1]!;
      writeFileSync(outPath, JSON.stringify(FAILING_OUT_JSON));
      return ok();
    };
    const result = runCli({ argv: argv(), env: {}, cwd: dir, spawnPromptfoo: spawn });
    expect(result.markdown).toMatch(/Score: 0\.00/);
    expect(result.markdown).toContain("MC-1");
    expect(readFileSync(result.reportPath, "utf8")).toBe(result.markdown);
  });

  it("reports a run failure — never the stale report — pins mutation #2 (fake a pass on run failure)", () => {
    const spawn: SpawnPromptfoo = () => ({ status: 1, stderr: "provider crashed" });
    const result = runCli({ argv: argv(), env: {}, cwd: dir, spawnPromptfoo: spawn });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(result.markdown).toContain("provider crashed");
  });

  it("reports a spawn error (the binary itself could not run) as a run failure", () => {
    const spawn: SpawnPromptfoo = () => ({ status: null, stderr: "", error: new Error("ENOENT") });
    const result = runCli({ argv: argv(), env: {}, cwd: dir, spawnPromptfoo: spawn });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(result.markdown).toContain("ENOENT");
  });

  // Critical 1: a crashed round must never be mistaken for a previous
  // round's success. promptfoo does not truncate `-o` up front, so a crash
  // before it writes leaves the PREVIOUS round's file sitting there for an
  // unguarded read to pick up as if it were fresh.
  it("never reads a stale out.json left behind by a previous round", () => {
    const outDir = join(dir, "out");
    mkdirSync(outDir, { recursive: true });
    const staleOutPath = join(outDir, "out.json");
    writeFileSync(staleOutPath, JSON.stringify(FAILING_OUT_JSON));

    // Simulates a crash: the spawn returns non-zero and writes nothing,
    // leaving the stale file exactly as a real crash would.
    const spawn: SpawnPromptfoo = () => ({ status: 1, stderr: "crashed before writing" });
    const result = runCli({ argv: argv(), env: {}, cwd: dir, spawnPromptfoo: spawn });

    expect(result.markdown).toMatch(/run itself failed/i);
    expect(result.markdown).not.toMatch(/Score: 0\.00 \*\* across 1 scenario/);
    expect(result.markdown).not.toContain("## Scenarios");
  });

  it("wraps a malformed --scenarios file: no throw, a run-failure report, exit content only", () => {
    writeFileSync(join(dir, "scenarios.json"), "{ not json");
    const spawn: SpawnPromptfoo = () => ok();
    expect(() => runCli({ argv: argv(), env: {}, cwd: dir, spawnPromptfoo: spawn })).not.toThrow();
    const result = runCli({ argv: argv(), env: {}, cwd: dir, spawnPromptfoo: spawn });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(existsSync(result.reportPath)).toBe(true);
  });

  it("wraps a missing --app: no throw, a run-failure report written under --out", () => {
    const spawn: SpawnPromptfoo = () => ok();
    const result = runCli({
      argv: ["--scenarios", join(dir, "scenarios.json"), "--out", join(dir, "out"), "--afm", afmPath()],
      env: {},
      cwd: dir,
      spawnPromptfoo: spawn,
    });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(result.reportPath.startsWith(join(dir, "out"))).toBe(true);
  });

  it("wraps a missing --out: no throw, falls back to writing the report under cwd", () => {
    const spawn: SpawnPromptfoo = () => ok();
    const result = runCli({
      argv: ["--scenarios", join(dir, "scenarios.json"), "--app", join(dir, "app"), "--afm", afmPath()],
      env: {},
      cwd: dir,
      spawnPromptfoo: spawn,
    });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(existsSync(result.reportPath)).toBe(true);
    expect(result.reportPath.startsWith(dir)).toBe(true);
  });

  it("falls back to cwd when --out itself is unwritable", () => {
    // A file where a directory is expected makes mkdirSync throw ENOTDIR.
    const blocked = join(dir, "blocked-out");
    writeFileSync(blocked, "not a directory");
    const spawn: SpawnPromptfoo = () => ok();
    const result = runCli({ argv: argv({ out: blocked }), env: {}, cwd: dir, spawnPromptfoo: spawn });
    expect(existsSync(result.reportPath)).toBe(true);
    expect(result.reportPath.startsWith(blocked)).toBe(false);
  });

  it("resolves relative --app/--out against cwd before handing them to the spawn (Important 4)", () => {
    let seenCwd = "";
    let seenConfigPath = "";
    let seenOutPath = "";
    const spawn: SpawnPromptfoo = (args, opts) => {
      seenCwd = opts.cwd;
      seenConfigPath = args[args.indexOf("-c") + 1]!;
      seenOutPath = args[args.indexOf("-o") + 1]!;
      writeFileSync(seenOutPath, JSON.stringify(FAILING_OUT_JSON));
      return ok();
    };
    runCli({
      argv: [
        "--scenarios", join(dir, "scenarios.json"),
        "--app", "app",
        "--out", "out",
        "--afm", afmPath(),
      ],
      env: {},
      cwd: dir,
      spawnPromptfoo: spawn,
    });
    expect(isAbsolute(seenCwd)).toBe(true);
    expect(isAbsolute(seenConfigPath)).toBe(true);
    expect(isAbsolute(seenOutPath)).toBe(true);
    expect(seenCwd).toBe(join(dir, "app"));
  });

  // The provider cannot receive a function through `vars`, so what it gets
  // instead is the agent it must boot and the contracts it must stub. An
  // emitted config without them is the vacuous run this task exists to end:
  // every scenario errors and nothing is actually evaluated.
  it("puts the app path and the declared tool contracts into the provider config", () => {
    let config: unknown;
    const spawn: SpawnPromptfoo = (args) => {
      const configPath = args[args.indexOf("-c") + 1]!;
      config = JSON.parse(readFileSync(configPath, "utf8")) as unknown;
      const outPath = args[args.indexOf("-o") + 1]!;
      writeFileSync(outPath, JSON.stringify(FAILING_OUT_JSON));
      return ok();
    };
    runCli({ argv: argv(), env: {}, cwd: dir, spawnPromptfoo: spawn });
    const provider = (config as { providers: Array<{ config: Record<string, unknown> }> })
      .providers[0]!;
    expect(provider.config.appDir).toBe(join(dir, "app"));
    // Without a bound the provider cannot set, a misconfigured agent is
    // diagnosed at the production default on every scenario of every round.
    expect(provider.config.firstBootTimeoutMs).toBe(20_000);
    expect(provider.config.readyTimeoutMs).toBe(60_000);
    expect(provider.config.toolStubs).toEqual([
      {
        envVar: "HOTEL_API_URL",
        specPath: join(dir, "specs", "design", "components", "hotel-api", "openapi.yaml"),
        // The security boundary travels with the contract, or the stub
        // world is more permissive than the agent it is testing.
        allow: ["listHotels"],
      },
    ]);
  });

  // The one thing that must never be in that file: it is written into the
  // build's output directory, which a PR may carry.
  it("never writes the model credential into the emitted config", () => {
    let text = "";
    const spawn: SpawnPromptfoo = (args) => {
      text = readFileSync(args[args.indexOf("-c") + 1]!, "utf8");
      writeFileSync(args[args.indexOf("-o") + 1]!, JSON.stringify(FAILING_OUT_JSON));
      return ok();
    };
    runCli({
      argv: argv(),
      env: { ANTHROPIC_API_KEY: "sk-ant-secret" },
      cwd: dir,
      spawnPromptfoo: spawn,
    });
    expect(text).not.toContain("sk-ant-secret");
  });

  it("reports a missing --afm as a run failure rather than evaluating an agent with no tools wired", () => {
    const spawn: SpawnPromptfoo = () => ok();
    const result = runCli({
      argv: ["--scenarios", join(dir, "scenarios.json"), "--app", join(dir, "app"), "--out", join(dir, "out")],
      env: {},
      cwd: dir,
      spawnPromptfoo: spawn,
    });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(result.markdown).toContain("--afm");
  });

  it("never forwards the parent's full environment to the child (Important 5)", () => {
    let seenEnv: NodeJS.ProcessEnv = {};
    const spawn: SpawnPromptfoo = (args, opts) => {
      seenEnv = opts.env;
      const outPath = args[args.indexOf("-o") + 1]!;
      writeFileSync(outPath, JSON.stringify(FAILING_OUT_JSON));
      return ok();
    };
    runCli({
      argv: argv(),
      env: { CLAUDE_CODE_OAUTH_TOKEN: "coding-token", RANDOM_SECRET: "x", PATH: "/usr/bin" },
      cwd: dir,
      spawnPromptfoo: spawn,
    });
    expect(seenEnv.CLAUDE_CODE_OAUTH_TOKEN).toBeUndefined();
    expect(seenEnv.RANDOM_SECRET).toBeUndefined();
    expect(seenEnv.PATH).toBe("/usr/bin");
  });
});
