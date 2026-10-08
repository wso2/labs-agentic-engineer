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
  resolveGrader,
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

const OLLAMA_ENV = {
  AEP_EVAL_KEY_MANAGED: "1",
  AEP_EVAL_MODEL_API_KEY: "ollama-key-value-0123",
  AEP_EVAL_MODEL_FORMAT: "openai-compatible",
  AEP_EVAL_MODEL_BASE_URL: "https://ollama.com/v1",
  AEP_EVAL_MODEL_NAME: "gpt-oss:20b",
  AEP_EVAL_MODEL_AUTH_SCHEME: "bearer",
};

describe("resolveGrader", () => {
  const FIRST_PARTY_JUDGE = {
    id: "anthropic:messages:claude-sonnet-5-5",
    config: { apiBaseUrl: "https://api.anthropic.com" },
  };

  it("defaults to Anthropic's API when the run has no connection", () => {
    expect(resolveGrader({})).toEqual(FIRST_PARTY_JUDGE);
  });

  it("follows the connection", () => {
    expect(resolveGrader(OLLAMA_ENV)).toEqual({
      id: "openai:chat:gpt-oss:20b",
      config: { apiBaseUrl: "https://ollama.com/v1" },
    });
  });

  // A bare `env.AGENT_EVAL_GRADER ?? default` would let a set-but-blank env
  // var through as `""`, which `buildPromptfooConfig` rejects outright.
  it("follows the connection when AGENT_EVAL_GRADER is set but blank", () => {
    expect(resolveGrader({ AGENT_EVAL_GRADER: "   " })).toEqual(FIRST_PARTY_JUDGE);
  });

  it("uses AGENT_EVAL_GRADER when genuinely set, over the connection", () => {
    expect(resolveGrader({ ...OLLAMA_ENV, AGENT_EVAL_GRADER: "openai:gpt-4o" })).toBe("openai:gpt-4o");
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

// Which key the run has, and from where, is resolveConnection's (see
// connection.test.ts); these pin what the promptfoo child is handed.
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
    expect(env.CLAUDE_CODE_OAUTH_TOKEN).toBeUndefined();
    expect(env.SOME_OTHER_SECRET).toBeUndefined();
    expect(env.PROMPTFOO_DISABLE_TELEMETRY).toBe("1");
    expect(env.PROMPTFOO_DISABLE_UPDATE).toBe("1");
    expect(env.PROMPTFOO_DISABLE_SHARING).toBe("1");
  });

  // A developer's key on Anthropic's API: the agent reads it as MODEL_API_KEY,
  // the judge under promptfoo's ANTHROPIC_API_KEY. One credential, two names.
  it("hands the developer's key to the agent and the judge, on Anthropic's API", () => {
    const env = buildChildEnv({ ANTHROPIC_API_KEY: "sk-ant-local" });
    expect(env).toMatchObject({
      ANTHROPIC_API_KEY: "sk-ant-local",
      MODEL_API_KEY: "sk-ant-local",
      MODEL_ENDPOINT: "https://api.anthropic.com/v1",
      MODEL_NAME: "claude-sonnet-5-5",
      MODEL_API_FORMAT: "anthropic",
      MODEL_API_AUTH_SCHEME: "x-api-key",
    });
  });

  it("hands the platform's connection to the agent and the judge, under the names each reads", () => {
    const env = buildChildEnv({ ...OLLAMA_ENV, ANTHROPIC_API_KEY: "sk-ant-the-orgs-coding-key" });
    expect(env).toMatchObject({
      MODEL_API_KEY: "ollama-key-value-0123",
      MODEL_ENDPOINT: "https://ollama.com/v1",
      MODEL_NAME: "gpt-oss:20b",
      MODEL_API_FORMAT: "openai-compatible",
      MODEL_API_AUTH_SCHEME: "bearer",
      OPENAI_API_KEY: "ollama-key-value-0123",
    });
    // The pod's coding key is nobody's credential here, and the connection's
    // key is not presented to a provider family that is not its format's.
    expect("ANTHROPIC_API_KEY" in env).toBe(false);
    expect("AEP_EVAL_MODEL_API_KEY" in env).toBe(false);
  });

  it("gives the child no model variables at all when the run has no key", () => {
    const env = buildChildEnv({ AEP_EVAL_KEY_MANAGED: "1", ANTHROPIC_API_KEY: "sk-ant-coding" });
    expect(Object.keys(env).filter((k) => k.startsWith("MODEL_") || k.includes("API_KEY"))).toEqual([]);
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

  it("never writes the connection's key into the emitted config", () => {
    let text = "";
    const spawn: SpawnPromptfoo = (args) => {
      text = readFileSync(args[args.indexOf("-c") + 1]!, "utf8");
      writeFileSync(args[args.indexOf("-o") + 1]!, JSON.stringify(FAILING_OUT_JSON));
      return ok();
    };
    runCli({ argv: argv(), env: OLLAMA_ENV, cwd: dir, spawnPromptfoo: spawn });
    expect(text).toContain("openai:chat:gpt-oss:20b");
    expect(text).not.toContain(OLLAMA_ENV.AEP_EVAL_MODEL_API_KEY);
  });

  // report.md and out.json land in the build's output and then in the PR.
  // The connection's key has no fixed shape, so it is scrubbed by value.
  it("scrubs the key by value from a graded report and from out.json", () => {
    const key = OLLAMA_ENV.AEP_EVAL_MODEL_API_KEY;
    const leaking = structuredClone(FAILING_OUT_JSON);
    leaking.results.results[0]!.gradingResult.componentResults[0]!.reason = `the agent printed ${key}`;
    const spawn: SpawnPromptfoo = (args) => {
      writeFileSync(args[args.indexOf("-o") + 1]!, JSON.stringify(leaking));
      return ok();
    };
    const result = runCli({ argv: argv(), env: OLLAMA_ENV, cwd: dir, spawnPromptfoo: spawn });
    expect(result.markdown).toContain("the agent printed «redacted»");
    expect(readFileSync(result.reportPath, "utf8")).not.toContain(key);
    expect(readFileSync(join(dir, "out", "out.json"), "utf8")).not.toContain(key);
  });

  it("scrubs the key by value from a run-failure report, and anything Anthropic-shaped", () => {
    const key = OLLAMA_ENV.AEP_EVAL_MODEL_API_KEY;
    const spawn: SpawnPromptfoo = () => ({ status: 1, stderr: `401 for ${key}; also sk-ant-api03-stray` });
    const result = runCli({ argv: argv(), env: OLLAMA_ENV, cwd: dir, spawnPromptfoo: spawn });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(result.markdown).not.toContain(key);
    expect(result.markdown).not.toContain("sk-ant-api03-stray");
  });

  // promptfoo would grade on "gpt-oss" — a model nobody chose — so the run
  // says why it cannot grade instead.
  it("reports an Anthropic-format judge it cannot name as a run failure", () => {
    const spawn: SpawnPromptfoo = () => ok();
    const result = runCli({
      argv: argv(),
      env: { ...OLLAMA_ENV, AEP_EVAL_MODEL_FORMAT: "anthropic" },
      cwd: dir,
      spawnPromptfoo: spawn,
    });
    expect(result.markdown).toMatch(/run itself failed/i);
    expect(result.markdown).toContain("AGENT_EVAL_GRADER");
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
