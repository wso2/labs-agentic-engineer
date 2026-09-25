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
 * SEEDING: give the empty database something to look at.
 *
 * A fresh wired session starts on an empty schema, and an app with no rows
 * shows every screen's empty state and nothing else — which is a poor way to
 * find out whether the screens work. Writing the calls by hand means reading
 * the contract and inventing plausible people, which is exactly the shape of
 * work a model is good at and a person resents.
 *
 * **The agent writes a SCRIPT, and the harness runs it.** That is the whole
 * boundary. The first seed costs a model call; every seed after it is `bash
 * seed.sh` with no model in the loop, reviewable and editable like any other
 * script, and diffable when it stops working. An agent that "seeded the
 * database" directly would leave nothing behind to read, repeat or correct.
 */

import { existsSync, readFileSync } from "node:fs";
import { wirePaths } from "../state.js";
import { run } from "../runtime.js";
import { runAgentTask } from "./task.js";
import type { WirePlan } from "../plan.js";

export interface SeedRequest {
  projectDir: string;
  plan: WirePlan;
  /** Where the calls go: the dev server, or the service itself. */
  proxyUrl: string;
  /** True when `proxyUrl` is the dev server, which enforces scopes in front of the service. */
  proxied: boolean;
  tokensFile: string;
  useApiKey?: boolean;
}

export interface SeedResult {
  ok: boolean;
  summary: string;
}

/** Replay the script if it exists, write it with a model first if it does not. */
export async function runSeedAgent(request: SeedRequest): Promise<SeedResult> {
  const paths = wirePaths(request.projectDir);
  if (!existsSync(paths.seed)) {
    const written = await writeSeedScript(request);
    if (!written.ok) return written;
  }
  return replaySeedScript(paths.seed, request.proxyUrl);
}

/** Run the script. The proxy URL rides in the environment so the script never hard-codes a port. */
async function replaySeedScript(script: string, proxyUrl: string): Promise<SeedResult> {
  const result = await run("bash", [script], {
    capture: true,
    env: { ...process.env, AEP_WIRED_URL: proxyUrl },
  });
  const tail = result.output.trimEnd().split("\n").slice(-3).join(" · ");
  return result.code === 0
    ? { ok: true, summary: `seeded — ${tail || "no output"}` }
    : { ok: false, summary: `seed.sh exited ${String(result.code)} — ${tail}` };
}

async function writeSeedScript(request: SeedRequest): Promise<SeedResult> {
  const paths = wirePaths(request.projectDir);
  const tokens = existsSync(request.tokensFile) ? readFileSync(request.tokensFile, "utf8") : "{}";
  const result = await runAgentTask({
    boundary: {
      task: "The seed task",
      slug: "seed",
      mayWrite: [paths.seed],
      // The ONE thing it may run: a call against the running app's own URL. Not
      // docker, not npm, not psql — the database is reached the way a user
      // reaches it, through the API, so seeded rows go through the same
      // validation and the same business rules a person would hit.
      mayCurl: request.proxyUrl,
    },
    prompt: seedPrompt(request, tokens),
    cwd: request.projectDir,
    maxTurns: 40,
    transcriptDir: paths.agents,
    ...(request.useApiKey ? { useApiKey: true } : {}),
  });

  if (!existsSync(paths.seed)) {
    return { ok: false, summary: `the seed task wrote no script (${result.text.slice(0, 200)})` };
  }
  return { ok: result.ok, summary: `wrote ${paths.seed}` };
}

function seedPrompt(request: SeedRequest, tokens: string): string {
  const paths = wirePaths(request.projectDir);
  return [
    "You are seeding a locally running generated application so a person can walk its screens with realistic data.",
    "",
    `The app answers at ${request.proxyUrl}. Its API is under ${request.proxyUrl}/api.`,
    "Every call must carry a role's bearer token, exactly as the browser sends it:",
    "",
    "```",
    tokens.trim(),
    "```",
    "",
    "The contracts are in specs/design/components/*/openapi.yaml and the roles and their grants are in",
    "specs/design/security.json. Read them.",
    request.proxied
      ? "The scopes are enforced in front of the service, so a call has to be made as a role that holds the operation's scope."
      : "There is no gateway in front of this service — the token identifies the caller and nothing checks scopes, so keep to calls a role could legitimately make anyway.",
    "",
    "YOUR ONLY OUTPUT IS ONE FILE:",
    "",
    `  ${paths.seed}`,
    "",
    "a bash script of curl calls that creates a small, coherent set of records — enough that every list screen has",
    "several rows and at least one interesting case (something overdue, something completed, something belonging to",
    "each role). Rules for the script:",
    "",
    "- It reads its base URL from $AEP_WIRED_URL, defaulting to the URL above.",
    "- It is idempotent: run twice, and there is not twice as much data. Check before creating.",
    "- It says what it did, one line per record.",
    "- It exits non-zero if a call fails.",
    "",
    "You may run curl against the app while drafting, to check a request shape. You may not start, stop or rebuild",
    "anything, and you may not edit the application or its specs — if a call comes back 4xx because the service or",
    "the contract is wrong, that is a FINDING: write it as a comment in the script and carry on with what does work.",
    "",
    "When the script is written, reply with one sentence saying what it seeds and anything you found.",
  ].join("\n");
}
