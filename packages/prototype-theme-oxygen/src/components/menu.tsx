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
 * A menu drawn in place, for the user menu and a row's overflow actions. It
 * stays inside the kit's scene (`disablePortal`), so Annotate reaches its
 * entries, and mounted while closed (`keepMounted`), so the render check sees
 * where every entry leads. Kept in place, the modal would hide its own
 * ancestors from assistive technology (its default container is the body): it
 * hides only the trigger beside it, and leaves the frame's scrolling alone.
 */

import { Menu } from "@wso2/oxygen-ui";
import type { ReactNode } from "react";

export interface InPlaceMenuProps {
  id: string;
  /** The trigger the menu opens from; null while closed. */
  anchor: HTMLElement | null;
  onClose: () => void;
  minWidth: number;
  children: ReactNode;
}

export function InPlaceMenu({ id, anchor, onClose, minWidth, children }: InPlaceMenuProps) {
  return (
    <Menu
      id={id}
      anchorEl={anchor}
      open={anchor !== null}
      onClose={onClose}
      disablePortal
      keepMounted
      container={() => anchor?.parentElement ?? null}
      disableScrollLock
      anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
      transformOrigin={{ vertical: "top", horizontal: "right" }}
      slotProps={{ paper: { sx: { minWidth, mt: 1 } } }}
    >
      {children}
    </Menu>
  );
}
