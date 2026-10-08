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

import { useBlocker } from "@tanstack/react-router";
import { Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle } from "@wso2/oxygen-ui";

/**
 * A card's unsaved draft, kept from being lost by mistake: while `dirty`,
 * leaving the card (its close, Escape, the scrim, the rail, the browser's
 * back) asks first, and so does reloading the page. Leaving on purpose (after
 * a Save or a Delete) navigates with `ignoreBlocker`.
 */
export function LeaveGuard({ dirty, what }: { dirty: boolean; /** "this skill". */ what: string }) {
  const blocker = useBlocker({
    shouldBlockFn: ({ current, next }) => dirty && current.pathname !== next.pathname,
    enableBeforeUnload: dirty,
    withResolver: true,
  });
  if (blocker.status !== "blocked") return null;
  return (
    <Dialog open onClose={blocker.reset} maxWidth="xs" fullWidth>
      <DialogTitle>Discard your changes?</DialogTitle>
      <DialogContent>
        <DialogContentText>Your changes to {what} are not saved. Leaving drops them.</DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={blocker.reset}>Keep editing</Button>
        <Button color="error" variant="contained" onClick={blocker.proceed}>
          Discard
        </Button>
      </DialogActions>
    </Dialog>
  );
}
