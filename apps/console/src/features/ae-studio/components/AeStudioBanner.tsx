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

import { Alert } from "@wso2/oxygen-ui";
import { useAeStudioBanner } from "./AeStudioGate";

// The shell's notice while AE Studio starts behind a visible console: "starting"
// until this session has seen it ready, "restarting" (after a settings change)
// once it has. The console stays usable; only pod-backed surfaces wait. A plain Alert, not
// Oxygen's NotificationBanner: that one always renders a close button, and
// this notice is not the user's to dismiss — it ends when the restart does.
export function AeStudioBanner() {
  const banner = useAeStudioBanner();
  if (!banner) return null;
  return (
    <Alert severity="info" sx={{ borderRadius: 0, flexShrink: 0 }}>
      {banner === "restarting" ? "AE Studio is restarting…" : "AE Studio is starting…"}
    </Alert>
  );
}
