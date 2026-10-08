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
 * The render check as it runs inside its isolated context: run the prototype
 * module, then draw every manifest screen for every role that reaches it in
 * every display state, and read the markup for what a static check cannot
 * see — a screen that throws, two elements sharing an id, a `to` the role
 * cannot follow. A finding is reported once per screen, at the first role and
 * state that shows it.
 */

import { renderToString } from "react-dom/server";
import type { PrototypeApp } from "../app.js";
import { SOURCE_FILE, type Finding } from "../findings.js";
import type { PrototypeManifest } from "../manifest/types.js";
import type { KitView } from "../runtime/context.js";
import { KitRoot } from "../runtime/KitRoot.js";
import { runPrototypeModule, type ModuleFactory } from "../runtime/modules.js";
import type { PrototypeTheme } from "../theme/contract.js";

/** How many findings the check reports before it stops looking. */
const MAX_FINDINGS = 40;

const noop = () => {};

function attributeValues(html: string, name: string): string[] {
  const out: string[] = [];
  const re = new RegExp(`${name}="([^"]*)"`, "g");
  for (let m = re.exec(html); m !== null; m = re.exec(html)) out.push(unescapeHtml(m[1]!));
  return out;
}

function unescapeHtml(s: string): string {
  return s.replace(/&quot;/g, '"').replace(/&#x27;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export function checkRender(manifest: PrototypeManifest, factory: ModuleFactory, theme: PrototypeTheme): Finding[] {
  const findings: Finding[] = [];
  const seen = new Set<string>();
  const report = (code: Finding["code"], location: string, text: string, screenId: string) => {
    const key = `${code}\n${screenId}\n${text}`;
    if (seen.has(key) || findings.length >= MAX_FINDINGS) return;
    seen.add(key);
    findings.push({ code, file: SOURCE_FILE, location, message: text });
  };

  let app: PrototypeApp;
  try {
    app = runPrototypeModule(factory);
  } catch (e) {
    const text = message(e);
    return [{ code: text.includes("export default defineApp") ? "NO_APP" : "RENDER_FAILED", file: SOURCE_FILE, location: "module", message: text }];
  }

  const declared = new Set(manifest.screens.map((s) => s.id));
  for (const id of Object.keys(app.screens)) {
    if (!declared.has(id)) report("SCREEN_MISMATCH", id, `defineApp draws screen ${JSON.stringify(id)}, which prototype.json does not list`, id);
  }
  for (const s of manifest.screens) {
    if (!Object.hasOwn(app.screens, s.id)) report("SCREEN_MISMATCH", s.id, `prototype.json lists screen ${JSON.stringify(s.id)}, which defineApp does not draw`, s.id);
  }

  for (const screen of manifest.screens) {
    if (!Object.hasOwn(app.screens, screen.id)) continue;
    for (const roleId of screen.roleIds) {
      const reachable = new Set(manifest.screens.filter((s) => s.roleIds.includes(roleId)).map((s) => s.id));
      for (const state of manifest.states) {
        const where = `${screen.id} as ${roleId} in ${state.id}`;
        const view: KitView = { mode: "preview", roleId, stateId: state.id, screenId: screen.id, selectedKeys: [], pins: {} };
        let html: string;
        try {
          html = renderToString(<KitRoot app={app} manifest={manifest} theme={theme} view={view} onNavigate={noop} onToggle={noop} />);
        } catch (e) {
          report("RENDER_FAILED", where, message(e), screen.id);
          continue;
        }
        const keys = new Set<string>();
        for (const key of attributeValues(html, "data-proto-key")) {
          if (keys.has(key)) report("DUPLICATE_ELEMENT_ID", where, `id ${JSON.stringify(key)} is used by two elements on this screen; element ids are unique per screen`, screen.id);
          keys.add(key);
        }
        for (const to of new Set(attributeValues(html, "data-proto-to"))) {
          if (!declared.has(to)) {
            report("UNKNOWN_NAV_TARGET", where, `to=${JSON.stringify(to)} is not one of the manifest's screens`, screen.id);
          } else if (!reachable.has(to)) {
            report("UNKNOWN_NAV_TARGET", where, `to=${JSON.stringify(to)} is a screen role ${JSON.stringify(roleId)} does not reach; hide the control for that role or add the role to the screen`, screen.id);
          }
        }
      }
    }
  }
  return findings;
}
