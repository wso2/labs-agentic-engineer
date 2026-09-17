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
 * One cell of the permission matrix: does this role grant this handle?
 *
 * It is a checkbox rather than a styled span because it IS one — the design's
 * filled and hollow marks are a two-state control, and the platform's own
 * control already carries the keyboard handling, the focus ring and the
 * accessible name a reader with a screen reader needs to tell a granted cell
 * from an empty one. The marks are only its icons.
 */

import { Checkbox, Tooltip } from "@wso2/oxygen-ui";
import { Circle } from "@wso2/oxygen-ui-icons-react";

/** The design's ○ — declared, not granted. */
const HOLLOW = <Circle size={15} aria-hidden />;
/** The design's ● — granted. */
const FILLED = <Circle size={15} fill="currentColor" aria-hidden />;

export function GrantCell({
  role,
  handle,
  granted,
  disabledReason,
  onToggle,
}: {
  role: string;
  /** The full `<resource>:<action>` handle this column/row crosses at. */
  handle: string;
  granted: boolean;
  /** Why the cell cannot be edited, or undefined when it can. */
  disabledReason?: string | undefined;
  onToggle: (next: boolean) => void;
}) {
  const disabled = disabledReason !== undefined;
  const control = (
    <Checkbox
      size="small"
      checked={granted}
      disabled={disabled}
      onChange={(event) => onToggle(event.target.checked)}
      icon={HOLLOW}
      checkedIcon={FILLED}
      inputProps={{
        "aria-label": `${role} grants ${handle}`,
      }}
      sx={{ p: 0.5 }}
    />
  );
  if (!disabled) return control;
  // A disabled control fires no pointer events of its own, so the tooltip has
  // to hang on something that does.
  return (
    <Tooltip title={disabledReason}>
      <span>{control}</span>
    </Tooltip>
  );
}
