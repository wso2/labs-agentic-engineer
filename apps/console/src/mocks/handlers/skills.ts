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

import { http, HttpResponse } from "msw";
import type { components } from "../../generated/aep-api";
import { md, seedSkills, seedSkillUpdates, type MockSkill } from "../fixtures/skills";

type SkillDetailBody = components["schemas"]["SkillDetailBody"];
type SkillSummary = components["schemas"]["SkillSummary"];
type SkillUpdate = components["schemas"]["SkillUpdate"];
type CreateSkillInput = components["schemas"]["CreateSkillInput"];
type UpdateSkillInput = components["schemas"]["UpdateSkillInput"];

// The org's skill library in mock mode, kept for the browser session: what
// the Skills Page and the Skill card read and write. A reload starts over.

let skills = new Map<string, MockSkill>();
let updates: SkillUpdate[] = [];
let initialized = false;

function ensure() {
  if (initialized) return;
  initialized = true;
  skills = new Map(Object.entries(seedSkills).map(([name, s]) => [name, { ...s }]));
  updates = seedSkillUpdates.map((u) => ({ ...u }));
}

function sha(text: string): string {
  let h = 0;
  for (let i = 0; i < text.length; i++) h = (h * 31 + text.charCodeAt(i)) | 0;
  return (h >>> 0).toString(16);
}

function descriptionOf(skillMd: string): string {
  return /^description:\s*"?(.*?)"?\s*$/m.exec(skillMd)?.[1] ?? "";
}

function summary(name: string, s: MockSkill): SkillSummary {
  const editable = s.kind !== "platform";
  return {
    name,
    kind: s.kind,
    description: descriptionOf(s.skillMd),
    contentSha: sha(s.skillMd + JSON.stringify(s.references)),
    editable,
    deletable: editable,
    enabled: s.enabled,
    required: s.required,
  };
}

function detail(name: string, s: MockSkill): SkillDetailBody {
  return {
    ...summary(name, s),
    orgId: "acme",
    skillMd: s.skillMd,
    references: s.references,
    binaryReferences: [],
    updatedAt: new Date().toISOString(),
  };
}

function refused(message: string, status = 400) {
  return HttpResponse.json({ code: "SKILL_INVALID", message }, { status });
}

function frontmatterName(skillMd: string): string | undefined {
  return /^name:\s*(\S+)\s*$/m.exec(skillMd)?.[1];
}

export const skillsHandlers = [
  http.get("*/api/v1/skills", () => {
    ensure();
    return HttpResponse.json({
      repoUrl: "https://github.com/acme-dev/acme-skills",
      skills: [...skills].map(([name, s]) => summary(name, s)),
    });
  }),

  http.get("*/api/v1/skills/updates", () => {
    ensure();
    return HttpResponse.json({ updates, count: updates.length });
  }),

  // Every skill the org never edited takes the platform's version; the rest stay.
  http.post("*/api/v1/skills/sync", () => {
    ensure();
    const taken = updates.filter((u) => u.state === "update");
    updates = updates.filter((u) => u.state !== "update");
    return HttpResponse.json({ status: "synced", updated: taken.length });
  }),

  http.post("*/api/v1/skills/import", () => {
    ensure();
    const name = "slack-notify";
    if (skills.has(name)) return refused(`a skill named "${name}" already exists`, 409);
    skills.set(name, {
      kind: "imported",
      required: false,
      enabled: true,
      references: {},
      skillMd: md(name, "Post a message to a Slack channel.", "# Slack\n\nUse an incoming webhook.\n"),
    });
    return HttpResponse.json({ name, kind: "imported", license: "MIT", warnings: [] }, { status: 201 });
  }),

  http.get("*/api/v1/skills/:name", ({ params }) => {
    ensure();
    const name = String(params.name);
    const s = skills.get(name);
    if (!s) return refused(`skill "${name}" not found`, 404);
    return HttpResponse.json(detail(name, s));
  }),

  http.post("*/api/v1/skills", async ({ request }) => {
    ensure();
    const body = (await request.json()) as CreateSkillInput;
    if (skills.has(body.name)) return refused(`a skill named "${body.name}" already exists`, 409);
    if (frontmatterName(body.skillMd) !== body.name) return refused("the frontmatter name must equal the request name");
    const s: MockSkill = { kind: "org", required: false, enabled: true, references: body.references ?? {}, skillMd: body.skillMd };
    skills.set(body.name, s);
    return HttpResponse.json(detail(body.name, s), { status: 201 });
  }),

  http.put("*/api/v1/skills/:name", async ({ params, request }) => {
    ensure();
    const name = String(params.name);
    const s = skills.get(name);
    if (!s) return refused(`skill "${name}" not found`, 404);
    if (s.kind === "platform") return refused("the platform's skills are read-only", 403);
    const body = (await request.json()) as UpdateSkillInput;
    if (frontmatterName(body.skillMd) !== name) return refused("cannot rename a skill via update");
    const next = { ...s, skillMd: body.skillMd, references: body.references ?? {} };
    skills.set(name, next);
    // An edit to a skill the platform still offers an update for makes the two diverge.
    updates = updates.map((u) => (u.name === name && u.state === "update" ? { ...u, state: "conflict" } : u));
    return HttpResponse.json(detail(name, next));
  }),

  http.patch("*/api/v1/skills/:name", async ({ params, request }) => {
    ensure();
    const name = String(params.name);
    const s = skills.get(name);
    if (!s) return refused(`skill "${name}" not found`, 404);
    const { enabled } = (await request.json()) as { enabled: boolean };
    if (!enabled && s.required) return refused(`${name} is required by every run`, 409);
    const next = { ...s, enabled };
    skills.set(name, next);
    return HttpResponse.json(detail(name, next));
  }),

  http.delete("*/api/v1/skills/:name", ({ params }) => {
    ensure();
    const name = String(params.name);
    const s = skills.get(name);
    if (!s) return refused(`skill "${name}" not found`, 404);
    if (s.kind === "platform") return refused("the platform's skills cannot be deleted", 403);
    skills.delete(name);
    updates = updates.filter((u) => u.name !== name);
    return HttpResponse.json({});
  }),
];
