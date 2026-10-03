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

/** The application window a prototype renders in: a browser-style title and address bar, so where the prototype ends and the host begins is never in doubt. */

import type { ReactNode } from "react";

export function BrowserWindow({ title, address, children }: { title: string; address: string; children: ReactNode }) {
  return (
    <section className="ph-window" aria-label={`${title} prototype`}>
      <div className="ph-window-bar">
        <span className="ph-window-dots" aria-hidden>
          <i />
          <i />
          <i />
        </span>
        <span className="ph-window-title">{title}</span>
        <span className="ph-window-address" aria-label="Address">
          {address}
        </span>
      </div>
      <div className="ph-window-body">{children}</div>
    </section>
  );
}
