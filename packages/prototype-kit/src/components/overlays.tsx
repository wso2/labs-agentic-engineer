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
 * Overlays: dialogs and drawers, opened and closed by the screen's own state
 * (`const [open, setOpen] = useState(false)`). The body is the selectable
 * part: clicking it in Annotate selects the overlay itself.
 */

import type { ReactNode } from "react";
import { useKit } from "../runtime/context.js";
import { SelectableBox, requireId } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";

export interface DialogProps {
  id: string;
  title: string;
  open: boolean;
  onClose: () => void;
  children?: ReactNode;
  /** Its Buttons, at the bottom. */
  actions?: ReactNode;
}

export interface ThemeDialogProps {
  title: string;
  /** The title element's DOM id, for `aria-labelledby`. */
  titleId: string;
  open: boolean;
  /** Close on Escape (a theme calls `preventDefault()` on the Escape it uses, so the frame does not pass it to the host), on the backdrop, or on a close control. */
  onClose: () => void;
  children?: ReactNode;
  actions?: ReactNode;
}

/** A modal. */
export function Dialog({ id, title, open, onClose, children, actions }: DialogProps) {
  const { view } = useKit();
  const Themed = useThemed("Dialog");
  requireId("Dialog", id);
  return (
    <Themed title={title} titleId={`proto-dialog-${id}`} open={open} onClose={() => view.mode === "preview" && onClose()} actions={actions}>
      <SelectableBox id={id} label={title} container>
        {children}
      </SelectableBox>
    </Themed>
  );
}

export interface DrawerProps {
  id: string;
  title: string;
  open: boolean;
  onClose: () => void;
  children?: ReactNode;
}

export interface ThemeDrawerProps {
  title: string;
  open: boolean;
  onClose: () => void;
  children?: ReactNode;
}

/** A side panel. */
export function Drawer({ id, title, open, onClose, children }: DrawerProps) {
  const { view } = useKit();
  const Themed = useThemed("Drawer");
  requireId("Drawer", id);
  return (
    <Themed title={title} open={open} onClose={() => view.mode === "preview" && onClose()}>
      <SelectableBox id={id} label={title} container>
        {children}
      </SelectableBox>
    </Themed>
  );
}
