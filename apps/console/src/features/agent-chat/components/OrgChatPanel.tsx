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

import { Box, IconButton, InputBase, Tooltip, Typography } from "@wso2/oxygen-ui";
import { PanelLeftClose } from "@wso2/oxygen-ui-icons-react";
import { useSession } from "../../../auth/SessionContext";

/**
 * The org's chat, beside an org Page (the Dashboard, Projects, New project):
 * the chat follows the entity in view, and here that is the organization. It
 * has no conversation or agent behind it yet, so it says what it will be for
 * and its composer is disabled; nothing is sent or stored.
 */
export function OrgChatPanel({ onClose }: { onClose: () => void }) {
  const { orgHandle } = useSession();
  return (
    <Box component="aside" aria-label="Agent chat" sx={{ height: "100%", display: "flex", flexDirection: "column", minWidth: 0 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, pl: 1.75, pr: 1.5, py: 1.25, borderBottom: 1, borderColor: "divider" }}>
        <Typography noWrap sx={{ flex: 1, minWidth: 0, fontSize: "0.8125rem", fontWeight: 600 }}>
          {orgHandle ?? "Organization"}
        </Typography>
        <Tooltip title="Hide agent chat">
          <IconButton size="small" aria-label="Hide agent chat" onClick={onClose}>
            <PanelLeftClose size={16} />
          </IconButton>
        </Tooltip>
      </Box>
      <Box sx={{ flex: 1, minHeight: 0, display: "grid", placeItems: "center", px: 3 }}>
        <Typography variant="body2" color="text.secondary" sx={{ textAlign: "center", maxWidth: 280 }}>
          Ask about your organization: its projects, skills and resources. Coming soon.
        </Typography>
      </Box>
      <Box sx={{ borderTop: 1, borderColor: "divider", px: 1.5, pt: 1.25, pb: 1.5 }}>
        <Typography variant="caption" color="text.secondary" component="p" sx={{ mb: 0.75, ml: 0.25 }}>
          Talking about{" "}
          <Box component="b" sx={{ color: "text.primary", fontWeight: 600 }}>
            your organization
          </Box>
          . Not available yet.
        </Typography>
        <Box sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, bgcolor: "action.disabledBackground", py: 0.75, px: 1.5 }}>
          <InputBase fullWidth disabled placeholder="The organization's chat is coming soon" inputProps={{ "aria-label": "Message the agent" }} />
        </Box>
      </Box>
    </Box>
  );
}
