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
 * The planner: reads a saved case's specs and writes the checklist every
 * attempt at that case is walked against. Runs once at save; the checklist is
 * frozen (ADR-0004).
 */

import { z } from "zod";
import { ItemSchema, SPECIAL_ROLES, type Item } from "./case.js";
import { MODELS, PLANNER, TIMEOUTS } from "./config.js";
import { WIRED_AUTH_SEMANTICS } from "./wired-auth.js";
import { runSession, throwIfCredentialRefused } from "./session.js";

/** What the planner answers: the items, plus what it could not make walkable. */
const PlanSchema = z
  .object({
    items: z.array(ItemSchema).min(1),
    gaps: z.array(z.string()),
  })
  .strict();
export type Plan = z.infer<typeof PlanSchema>;

const SYSTEM_PROMPT = `You write acceptance checklists for web applications from their specifications.
You read files; you never write or run anything. You answer with one JSON object in the requested schema.`;

export function plannerPrompt(roles: string[]): string {
  return `This directory holds the specification of a web application that has NOT been built yet.
Write the acceptance checklist a tester will walk against the finished app.

READ (Glob/Grep/Read), in roughly this order:
- specs/requirements/prd.md — the user stories (numbered) and the assumptions.
- specs/design/components/*/wireframes.dsl — the screens and flows. \`screen <Name>\` blocks draw a screen; \`-> <Screen>\` on a control or table is where it navigates; \`flow\` blocks list, per role, the screens that role walks, entry screen first.
- specs/design/security.json — the roles and their grants.
- specs/design/components/*/openapi.yaml — the API contract, to learn what data the app can create and what it cannot.
- specs/design/flows/*.md and specs/design/domain-model.md — the key flows and the data model.
Skip *.excalidraw, *.gen.json and design.cell: they are renderings of the files above.

THE TESTER, and what that rules out:
- They walk the RUNNING app in a real browser and never see source code.
- The database starts EMPTY. Every row an item needs must be created through the UI by an EARLIER item in your list. Data persists across role switches (it is a real backend).
- They switch identity by URL only: \`?role=<Role>\` signs in as that role. The role names you may use in \`role\` are EXACTLY: ${[...roles, ...SPECIAL_ROLES].map((r) => JSON.stringify(r)).join(", ")}. "no role" is signed in holding no role at all; "signed out" is nobody signed in.

${WIRED_AUTH_SEMANTICS}

RULES
1. One item per user-visible behaviour: each screen reached through its flow, each control that changes something (create, edit, delete, approve, filter, export…), and each user story's observable outcome. Do not merge two behaviours into one item, and do not split one into several.
2. Order matters. Put items that create data before items that read or act on it (submit before approve, approve before export). Keep a role's items together where the data allows it. When an item relies on an earlier one's data, say so in \`steps\` ("the claim created in employee-submits-claim").
3. If the spec gives NO UI way to establish a precondition from an empty database (e.g. who reports to whom, reference data nobody can create), do not write an item that needs it. Nothing is seeded — not even a relationship between the test users in security.json — so "assume X is already set up" is never allowed. Write what an empty database plus the UI CAN show (the screen loads, its empty state reads sensibly) and record the precondition in \`gaps\`, one sentence each, naming the items a person could add by hand if the precondition turns out to hold.
4. Act only through what the wireframes.dsl draws. Editable: \`input\`, \`textarea\`, \`select\`, \`checkbox\`, \`search\`. Actionable: \`button\`, \`link\`, and anything carrying \`-> <Screen>\` (table rows, sidebar entries). Everything else — \`text\`, \`heading\`, \`badge\`, \`card\`, table cells — is READ-ONLY: an edit item changes only the fields its screen draws as editable. Where the PRD and the DSL disagree (a story says a field can be edited, the screen draws it as \`text\`), the DSL wins for the walk, and the disagreement goes into \`gaps\`.
5. The tester is already signed in as the item's \`role\` when its steps begin, on the screen that role lands on. \`steps\` therefore NEVER contain a URL, a query string or a sign-in action — start from the landing screen and use the app's own navigation.
6. Role reach. The app shows a role each screen whose data operation (in \`openapi.yaml\`) needs a scope that the role's grants in \`security.json\` hold, in one navigation for all roles. For every role: one item that it lands on its own entry screen. Expect the screen, not the navigation links that the wireframe draws for that role. Where a screen of another role loads an operation whose scope this role does not hold, one item that this role is refused that screen: the tester cannot know a route it never saw, so pick a screen an EARLIER item reached under its owning role, and write the step as "open the address the Approval Queue had in manager-lands-on-queue". When the role's grants reach every other screen, write no refusal item for it, and say so in \`gaps\`. One item as "no role": a no-access page instead of the app. One item as "signed out", expecting exactly what WIRED-MODE AUTH below says that caller sees.
7. One sign-out item: a signed-in role signs out through the app's sign-out control, expecting what WIRED-MODE AUTH says sign-out shows.
8. \`expect\` holds only what the spec states, or what any working app must do (no crash, no blank page, the new row is listed). Never invent a message, a status name or a behaviour the spec does not describe — when the spec is silent on an edge, leave that edge out. Expect the outcome, not the means: how the app shows, allows or blocks something is open unless the spec states it. A list with a filter shows only the rows the filter lets through: expect a row in it only when the item's steps set the filter to include that row, or when the spec states the filter's first value.
9. \`steps\`: concrete UI actions with concrete sample values ("Amount 42.50, Date 2026-03-01, Category Travel, Description 'Taxi to client'"). \`expect\`: what the tester can SEE — text, a row, a status badge, arriving on a named screen. Never mention source code, files, components, endpoints, status codes, or database state.
10. \`id\`: short, kebab-case, unique. \`screen\`: the wireframe screen name the item ends on. \`flow\`: the flow or story it belongs to, a few words. \`weight\`: 1, or 2 for the central action of a user story (the create, the approve, the export).
11. Cover every flow in every wireframes.dsl and every user story in the PRD that has a UI. Nothing beyond them.`;
}

/**
 * Derive a checklist. Retried ONCE with the reason when the answer fails the
 * schema or names a role `wire` would refuse — the SDK enforces the JSON shape,
 * but only this function knows the role list.
 */
export async function planChecklist(opts: {
  caseDir: string;
  roles: string[];
  env: NodeJS.ProcessEnv;
  transcriptFile: string;
  signal?: AbortSignal;
}): Promise<Plan & { costUsd: number | null }> {
  const base = plannerPrompt(opts.roles);
  let prompt = base;
  let costUsd: number | null = null;
  let lastError = "";
  for (let attempt = 1; attempt <= PLANNER.attempts; attempt += 1) {
    const result = await runSession({
      prompt,
      systemPrompt: SYSTEM_PROMPT,
      cwd: opts.caseDir,
      model: MODELS.planner,
      tools: PLANNER.tools,
      schema: PlanSchema,
      maxTurns: PLANNER.maxTurns,
      timeoutMs: TIMEOUTS.plannerMinutes * 60_000,
      env: opts.env,
      transcriptFile: opts.transcriptFile,
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
    throwIfCredentialRefused(result);
    if (result.costUsd !== null) costUsd = (costUsd ?? 0) + result.costUsd;
    if (result.output) {
      const problems = planProblems(result.output.items, opts.roles);
      if (problems.length === 0) return { ...result.output, costUsd };
      lastError = problems.join("; ");
    } else {
      lastError = result.error ?? "no answer";
    }
    prompt = `${base}\n\nYOUR PREVIOUS ANSWER WAS REJECTED: ${lastError}\nAnswer again, fixing that.`;
  }
  throw new Error(`the planner did not produce a valid checklist after ${String(PLANNER.attempts)} tries: ${lastError}`);
}

/** Why a plan cannot be saved as it stands: a role `wire` would refuse, or a duplicate id. */
export function planProblems(items: Item[], roles: string[]): string[] {
  const known = new Set<string>([...roles, ...SPECIAL_ROLES]);
  const problems: string[] = [];
  const seen = new Set<string>();
  for (const item of items) {
    if (!known.has(item.role)) problems.push(`item ${item.id} uses role "${item.role}", which is not one of the allowed names`);
    if (seen.has(item.id)) problems.push(`id ${item.id} is used twice`);
    seen.add(item.id);
  }
  return problems;
}
