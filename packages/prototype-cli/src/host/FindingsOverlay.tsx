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

/** The check's findings over the preview: a last good render, when there is one, stays underneath until the files are clean again. */

import type { Finding } from "@wso2/prototype-kit/check";

export function FindingsOverlay({ findings, showingLastGood }: { findings: readonly Finding[]; showingLastGood: boolean }) {
  return (
    <section className="ph-findings" role="region" aria-label="Check findings">
      <h2>
        {findings.length} check finding{findings.length === 1 ? "" : "s"}
        {showingLastGood ? " — showing the last good version" : ""}
      </h2>
      <ul>
        {findings.map((f, i) => (
          <li key={i}>
            <code>{f.code}</code> <span className="ph-where">{`${f.file} ${f.location}`}</span> {f.message}
          </li>
        ))}
      </ul>
    </section>
  );
}
