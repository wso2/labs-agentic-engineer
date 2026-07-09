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

// Materialises the per-task AgentSkills plugin tree under
// <workspace>/.aep/skills-plugin/.
//
// Layout (matches docs/design/skills-system.md > "Coding agent"):
//
//   .aep/skills-plugin/
//     .claude-plugin/
//       plugin.json                                 # {"name":"aep-task-skills","version":"1.0"}
//     skills/
//       org-api-management/
//         SKILL.md                                  # rewritten name: org-api-management
//         references/<file>.md                      # optional
//       org-go/
//         SKILL.md
//       custom-payments-pci-handling/
//         SKILL.md
//
// All kinds land in one plugin directory. The materialisation prefix
// (`org-`, `custom-`, `imported-`) is applied to both the directory
// name AND the `name:` frontmatter field; the original name is preserved
// under metadata.aep.canonical-name.

import fs from "node:fs";
import path from "node:path";

// SkillKind mirrors the platform's frontmatter `metadata.aep.kind` vocabulary
// (docs/design/skills-unified-library-migration.md §3.2); absent → "org".
export type SkillKind = "platform" | "org" | "custom" | "imported";

// SkillResolution is one applied skill resolved from the org-skills clone,
// ready to materialize into the AgentSkills plugin tree. Built locally by
// skills_resolver.ts (the retired S2S skills-pull returned the same shape over
// the wire).
export interface SkillResolution {
  materializedName: string; // e.g. "org-api-management" — kind-prefixed dir + `name:`
  kind: SkillKind; // frontmatter metadata.aep.kind (absent → "org")
  skillMd: string; // the SKILL.md body, verbatim
  references: Record<string, string>; // references/<file>.md → content
}

export interface MaterializeResult {
  pluginDir: string;
  preloadNames: string[]; // platform-shipped (org-kind) skills for the SDK `skills:` preload array
}

export async function materializeSkills(
  workspace: string,
  skills: SkillResolution[],
): Promise<MaterializeResult | null> {
  if (skills.length === 0) {
    return null;
  }
  const pluginDir = path.join(workspace, ".aep", "skills-plugin");
  const claudePluginDir = path.join(pluginDir, ".claude-plugin");
  const skillsDir = path.join(pluginDir, "skills");

  await fs.promises.mkdir(claudePluginDir, { recursive: true });
  await fs.promises.mkdir(skillsDir, { recursive: true });

  await fs.promises.writeFile(
    path.join(claudePluginDir, "plugin.json"),
    JSON.stringify({ name: "aep-task-skills", version: "1.0" }, null, 2) + "\n",
    { mode: 0o644 },
  );

  const preloadNames: string[] = [];

  for (const sk of skills) {
    const skillDir = path.join(skillsDir, sk.materializedName);
    await fs.promises.mkdir(skillDir, { recursive: true });

    const rewritten = rewriteSkillFrontmatter(sk.skillMd, sk.materializedName);
    await fs.promises.writeFile(path.join(skillDir, "SKILL.md"), rewritten, { mode: 0o644 });

    if (sk.references && Object.keys(sk.references).length > 0) {
      for (const [refPath, refBody] of Object.entries(sk.references)) {
        if (!refPath.startsWith("references/")) continue;
        if (refPath.includes("..")) continue; // safety
        const fullPath = path.join(skillDir, refPath);
        await fs.promises.mkdir(path.dirname(fullPath), { recursive: true });
        await fs.promises.writeFile(fullPath, refBody, { mode: 0o644 });
      }
    }

    // Platform-shipped stack skills (kind "org") are preloaded into the
    // session; the other kinds are available on-demand only.
    if (sk.kind === "org") {
      preloadNames.push(sk.materializedName);
    }
  }

  return { pluginDir, preloadNames };
}

// Rewrite the `name:` field in the SKILL.md frontmatter to the
// materialised name; preserve everything else verbatim. Also adds
// metadata.aep.canonical-name with the original name so any tooling
// that wants to find the source skill can.
//
// Quick frontmatter-only parse: we expect every SKILL.md to start with
// `---\n`. If it doesn't (defensive), bail out and write the body
// untouched.
export function rewriteSkillFrontmatter(skillMD: string, materializedName: string): string {
  const trimmed = skillMD.trimStart();
  if (!trimmed.startsWith("---")) {
    return skillMD; // no frontmatter — leave alone
  }
  const afterFirst = trimmed.indexOf("\n");
  if (afterFirst < 0) return skillMD;
  const endIdx = trimmed.indexOf("\n---", afterFirst);
  if (endIdx < 0) return skillMD;

  const fm = trimmed.slice(afterFirst + 1, endIdx);
  const body = trimmed.slice(endIdx + "\n---".length).replace(/^\r?\n/, "");

  const canonicalMatch = fm.match(/^name:\s*(.+)$/m);
  const canonicalName = canonicalMatch ? canonicalMatch[1].trim() : materializedName;

  // Replace existing name: line; if not present, prepend one.
  let newFm = fm;
  if (canonicalMatch) {
    newFm = newFm.replace(/^name:\s*.+$/m, `name: ${materializedName}`);
  } else {
    newFm = `name: ${materializedName}\n` + newFm;
  }

  // Stamp metadata.aep.canonical-name. If a metadata block exists,
  // merge into it; otherwise append a fresh one. Keeping this simple —
  // we don't claim YAML correctness for every edge case, just the
  // common shape our bootstrap writes.
  if (/^metadata:\s*$/m.test(newFm)) {
    const aepBlock = newFm.match(/^(\s+)aep:\s*$/m);
    if (aepBlock) {
      // Nested aep block already present — insert canonical-name as a
      // child so it isn't dropped.
      const childIndent = aepBlock[1] + "  ";
      newFm = newFm.replace(
        /^(\s+aep:\s*)$/m,
        `$1\n${childIndent}canonical-name: "${canonicalName}"`,
      );
    } else {
      newFm = newFm.replace(/^metadata:\s*$/m, `metadata:\n  aep:\n    canonical-name: "${canonicalName}"`);
    }
  } else {
    newFm = newFm + `\nmetadata:\n  aep:\n    canonical-name: "${canonicalName}"`;
  }

  return `---\n${newFm}\n---\n\n${body}`;
}
