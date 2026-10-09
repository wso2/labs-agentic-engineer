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
 * The dock's mode group (preview only, never in an export): the Preview ·
 * Comment tool pair (Comment is the reviewer's word for the view's Annotate
 * mode; `V` and `C` in the tooltips) and, in Comment mode, the hint that a
 * click comments (dropped on a compact dock: the window's tag still says it).
 */

import type { PrototypeViewEvent, PrototypeViewState } from "@wso2/prototype-kit/host";
import { DockGroup } from "./Dock.js";
import { COMMENT, Icon, POINTER } from "./icons.js";

export function ModeTools({ view, dispatch }: { view: PrototypeViewState; dispatch: (event: PrototypeViewEvent) => void }) {
  const commenting = view.mode === "annotate";
  return (
    <DockGroup label="Mode">
      <span className="ph-tools">
        <button type="button" title="Preview · V" aria-pressed={!commenting} onClick={() => dispatch({ type: "EXIT_ANNOTATE" })}>
          <Icon d={POINTER} />
          Preview
        </button>
        <button type="button" className="ph-tool-comment" title="Comment · C" aria-pressed={commenting} onClick={() => dispatch({ type: "ENTER_ANNOTATE" })}>
          <Icon d={COMMENT} />
          Comment
        </button>
      </span>
      {commenting && (
        <span className="ph-hint">
          <i aria-hidden />
          Click anything to comment
        </span>
      )}
    </DockGroup>
  );
}
