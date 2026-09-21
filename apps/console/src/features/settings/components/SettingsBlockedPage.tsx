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

import { EmptyState } from "../../../components/EmptyState";
import { NoPermissionIllustration } from "../../../components/NoPermissionIllustration";

// Bare /settings' terminal state: the caller holds none of Credentials',
// Skills', or Usage's permissions (see settings.index.tsx's
// resolveSettingsLandingPath), so there is no section worth landing on at
// all. Rendered into SettingsLayout's Outlet — the tab rail stays visible
// alongside it (every tab correctly disabled, Skills aside, which always
// self-gates its own content instead of the tab).
export function SettingsBlockedPage() {
  return (
    <EmptyState
      icon={<NoPermissionIllustration size={120} />}
      title="No settings access"
      description="You don't have permission to change settings that affect the entire organization."
    />
  );
}
