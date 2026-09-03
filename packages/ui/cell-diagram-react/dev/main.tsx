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

import { StrictMode, useCallback, useState } from "react";
import { createRoot } from "react-dom/client";
import { CellDiagram } from "../src/renderer/CellDiagram";
import type { DiagramTheme } from "../src/renderer/DiagramCanvas";
import type { CustomLayout } from "../src/renderer/customLayout";
import type { Diagnostic } from "../src/domain/cellModel";
import { defaultSampleSource } from "../src/test/defaultSample";
import "./playground.css";

/**
 * Dev-only harness: the package ships a library, so this is the one place the
 * diagram can be driven by hand — edit the DSL on the left, watch it render.
 * Never imported by `src/index.ts`; `pnpm dev` is its only entry point.
 */
function Playground() {
  const [source, setSource] = useState(defaultSampleSource);
  const [theme, setTheme] = useState<DiagramTheme>("light");
  const [tolerant, setTolerant] = useState(true);
  const [readOnly, setReadOnly] = useState(false);
  const [compact, setCompact] = useState(false);
  const [customLayout, setCustomLayout] = useState<CustomLayout | null>(null);
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);

  const onDiagnostics = useCallback((next: Diagnostic[]) => setDiagnostics(next), []);

  return (
    <div className="playground" data-theme={theme}>
      <aside className="playground__editor">
        <header className="playground__bar">
          <strong>cell DSL</strong>
          <span className="playground__spacer" />
          <button type="button" onClick={() => setCustomLayout(null)} disabled={!customLayout}>
            reset layout
          </button>
        </header>
        <textarea
          spellCheck={false}
          value={source}
          onChange={(event) => setSource(event.target.value)}
        />
        <footer className="playground__toggles">
          <label>
            <input
              type="checkbox"
              checked={theme === "dark"}
              onChange={(event) => setTheme(event.target.checked ? "dark" : "light")}
            />
            dark
          </label>
          <label>
            <input
              type="checkbox"
              checked={tolerant}
              onChange={(event) => setTolerant(event.target.checked)}
            />
            tolerant
          </label>
          <label>
            <input
              type="checkbox"
              checked={compact}
              onChange={(event) => setCompact(event.target.checked)}
            />
            compact
          </label>
          <label>
            <input
              type="checkbox"
              checked={readOnly}
              onChange={(event) => setReadOnly(event.target.checked)}
            />
            readOnly
          </label>
        </footer>
        <ul className="playground__diagnostics">
          {diagnostics.map((diagnostic, index) => (
            <li key={index} data-severity={diagnostic.severity}>
              {`line ${diagnostic.line}:${diagnostic.column} — ${diagnostic.message}`}
            </li>
          ))}
        </ul>
      </aside>
      <main className="playground__canvas">
        <CellDiagram
          source={source}
          theme={theme}
          tolerant={tolerant}
          compact={compact}
          readOnly={readOnly}
          customLayout={customLayout}
          onCustomLayoutChange={setCustomLayout}
          onDiagnostics={onDiagnostics}
        />
      </main>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Playground />
  </StrictMode>
);
