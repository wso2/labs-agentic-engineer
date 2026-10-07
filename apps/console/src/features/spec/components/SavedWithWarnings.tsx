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

import { Alert, AlertTitle, Box } from "@wso2/oxygen-ui";
import type { FlushWarning } from "../collab/specRoom";

/**
 * The Room's last save landed with warnings: each file and the platform's
 * message for it, as given. The title says the save happened, so a warning
 * is not read as lost work; the next save's warnings replace these.
 */
export function SavedWithWarnings({ warnings, onDismiss }: { warnings: FlushWarning[]; onDismiss: () => void }) {
  if (warnings.length === 0) return null;
  return (
    <Alert severity="warning" onClose={onDismiss} sx={{ mb: 2, maxWidth: "72ch" }}>
      <AlertTitle>Saved with warnings</AlertTitle>
      <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
        {warnings.map((w) => (
          <li key={w.path}>
            {w.path}: {w.message}
          </li>
        ))}
      </Box>
    </Alert>
  );
}
