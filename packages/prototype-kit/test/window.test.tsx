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

/** The prototype window's address follows the view: the screen, and the flow and display state when they are not the defaults. */

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PrototypeWindow, prototypeAddress } from "../src/host/PrototypeWindow.js";
import type { PrototypeManifest } from "../src/manifest/types.js";

const manifest = { states: [{ id: "state.default" }, { id: "state.empty" }] } as PrototypeManifest;

describe("prototypeAddress", () => {
  it("is prototype://<screenId> for the default state and no flow", () => {
    expect(prototypeAddress(manifest, { screenId: "screen.home", flowId: null, stateId: "state.default" })).toBe("prototype://screen.home");
  });

  it("adds the flow and a non-default display state as params", () => {
    expect(prototypeAddress(manifest, { screenId: "screen.home", flowId: "flow.a", stateId: "state.empty" })).toBe(
      "prototype://screen.home?flow=flow.a&state=state.empty",
    );
  });
});

describe("PrototypeWindow", () => {
  it("shows the title and the address around its children, with the host's class", () => {
    const html = renderToStaticMarkup(
      <PrototypeWindow title="Acme" manifest={manifest} view={{ screenId: "screen.home", flowId: null, stateId: "state.default" }} className="mine">
        <p>app</p>
      </PrototypeWindow>,
    );
    expect(html).toContain('class="proto-window mine"');
    expect(html).toContain("Acme");
    expect(html).toContain("prototype://screen.home");
    expect(html).toContain("<p>app</p>");
  });
});
