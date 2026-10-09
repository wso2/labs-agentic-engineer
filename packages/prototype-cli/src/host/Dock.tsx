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
 * The review's one floating dock: every control, in groups split by
 * dividers. It sits below the prototype window in the layout rather than over
 * it, so the space it takes is reserved and it never covers the prototype's
 * last rows; what a group opens (the comment list, the save's status) grows
 * upward from it, over the prototype.
 */

import { Children, Fragment, type ReactNode, type Ref } from "react";

/** One of the dock's groups, named for assistive technology. */
export function DockGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="ph-dock-group" role="group" aria-label={label}>
      {children}
    </div>
  );
}

export function Dock({ children, ref }: { children: ReactNode; /** The dock: a whole-screen comment's bubble points at it. */ ref?: Ref<HTMLElement> | undefined }) {
  return (
    <section ref={ref} className="ph-dock" aria-label="Review controls">
      {Children.toArray(children).map((group, i) => (
        <Fragment key={i}>
          {i > 0 && <span className="ph-dock-divider" aria-hidden />}
          {group}
        </Fragment>
      ))}
    </section>
  );
}
