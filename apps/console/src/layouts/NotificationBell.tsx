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

import {
  Badge,
  Box,
  Button,
  IconButton,
  NotificationPanel,
  Tooltip,
  Typography,
  formatRelativeTime,
  useAppShell,
} from "@wso2/oxygen-ui";
import { Bell } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import { useRecentAlerts } from "../features/alerts/api/queries";
import { useAlertsUnread } from "../features/alerts/hooks/useAlertsUnread";
import { classificationLabel } from "../features/alerts/classification";
import { attentionDescription, attentionLabel } from "../features/issues/attention";
import { useAttentionUnread } from "../features/issues/hooks/useAttentionUnread";

// Top-nav notification bell (#154) — global, read-only, client-tracked
// unread state (no server read-state; see the issue's grilling decisions).
// Must be a child of AppShell to reach useAppShell()'s panel toggle.
export function NotificationButton() {
  const { actions } = useAppShell();
  const { data: reports = [] } = useRecentAlerts();
  const alertsUnread = useAlertsUnread(reports);
  const attentionUnread = useAttentionUnread(reports);
  const unreadCount = alertsUnread.unreadCount + attentionUnread.unreadCount;

  return (
    <Tooltip title="Alerts">
      <IconButton
        onClick={() => {
          actions.toggleNotificationPanel();
          alertsUnread.markAllSeen();
          attentionUnread.markAllSeen();
        }}
        size="small"
        sx={{ color: "text.secondary" }}
        aria-label="Alerts"
      >
        <Badge badgeContent={unreadCount} color="error" max={99} invisible={unreadCount === 0}>
          <Bell size={20} />
        </Badge>
      </IconButton>
    </Tooltip>
  );
}

// Panel body — no per-item read state (the badge clears as a whole on open,
// per #154's decision), so this only needs the report list itself.
export function AlertsNotificationPanel() {
  const navigate = useNavigate();
  const { actions } = useAppShell();
  const { data: reports = [], isPending, isError, error, refetch } = useRecentAlerts();
  const attention = useAttentionUnread(reports);

  const openAlert = (alertId: string) => {
    // Close the overlay so it doesn't linger over the destination page.
    actions.toggleNotificationPanel();
    void navigate({ to: "/alerts/$alertId", params: { alertId } });
  };
  const openIssues = (projectName: string) => {
    actions.toggleNotificationPanel();
    void navigate({ to: "/projects/$projectName/issues", params: { projectName } });
  };

  return (
    <NotificationPanel>
      <NotificationPanel.Header>
        <NotificationPanel.HeaderIcon>
          <Bell size={20} />
        </NotificationPanel.HeaderIcon>
        <NotificationPanel.HeaderTitle>Alerts</NotificationPanel.HeaderTitle>
        <NotificationPanel.HeaderClose />
      </NotificationPanel.Header>
      {isPending ? (
        <NotificationPanel.EmptyState />
      ) : isError && reports.length === 0 ? (
        // Initial load failed with no last-known data to fall back on —
        // surface it distinctly from "no alerts yet" (api-guidelines: every
        // view ships an error state, not just empty/loading).
        <Box sx={{ px: 3, py: 4, textAlign: "center" }}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            Failed to load alerts
            {error instanceof Error && error.message ? `: ${error.message}` : ""}
          </Typography>
          <Button size="small" onClick={() => void refetch()}>
            Retry
          </Button>
        </Box>
      ) : reports.length === 0 && attention.items.length === 0 ? (
        <NotificationPanel.EmptyState />
      ) : (
        <>
          {attention.failedProjects.length > 0 && (
            // An issue list that failed to load hides that project's
            // attention items; say so instead of implying there are none.
            <Box sx={{ px: 3, py: 1.5, display: "flex", alignItems: "center", gap: 1.5 }}>
              <Typography variant="body2" color="text.secondary" sx={{ flex: 1 }}>
                Failed to load issue attention for {attention.failedProjects.join(", ")}
              </Typography>
              <Button size="small" onClick={attention.retryFailed}>
                Retry
              </Button>
            </Box>
          )}
          <NotificationPanel.List>
            {attention.items.map((item) => (
              <NotificationPanel.Item
                key={item.id}
                id={item.id}
                type={item.reason === "escalated" ? "error" : "warning"}
                read
              >
                <NotificationPanel.ItemTitle>
                  {attentionLabel(item.reason)}: #{item.issueNumber} {item.title}
                </NotificationPanel.ItemTitle>
                <NotificationPanel.ItemMessage>
                  {item.projectName} · {attentionDescription(item.reason)}
                </NotificationPanel.ItemMessage>
                <NotificationPanel.ItemAction onClick={() => openIssues(item.projectName)}>
                  Review
                </NotificationPanel.ItemAction>
              </NotificationPanel.Item>
            ))}
            {reports.map((report) => (
              <NotificationPanel.Item key={report.id} id={report.id!} type="info" read>
                <NotificationPanel.ItemTitle>{report.title}</NotificationPanel.ItemTitle>
                <NotificationPanel.ItemMessage>
                  {report.project} · {classificationLabel(report.classification)}
                </NotificationPanel.ItemMessage>
                <NotificationPanel.ItemTimestamp>
                  {report.createdAt ? formatRelativeTime(new Date(report.createdAt)) : ""}
                </NotificationPanel.ItemTimestamp>
                <NotificationPanel.ItemAction onClick={() => openAlert(report.id!)}>
                  View
                </NotificationPanel.ItemAction>
              </NotificationPanel.Item>
            ))}
          </NotificationPanel.List>
        </>
      )}
    </NotificationPanel>
  );
}
