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

import { createLink } from "@tanstack/react-router";
import { Alert, Box, ButtonBase, Chip, Skeleton, Typography } from "@wso2/oxygen-ui";
import type { AlertItem } from "../../issues/model/issues";
import { useAlerts } from "../../issues/useAlerts";

const RowLink = createLink(ButtonBase);

const ROW_SX = {
  display: "flex",
  alignItems: "baseline",
  gap: 1.5,
  justifyContent: "stretch",
  textAlign: "start",
  width: "100%",
  px: 1.75,
  py: 1.25,
  "& + &": { borderTop: 1, borderColor: "divider" },
} as const;

function AlertRow({ item }: { item: AlertItem }) {
  const content = (
    <>
      <Box component="span" sx={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
        <Typography component="span" variant="caption" color="text.secondary">
          {item.projectName}
          {item.issueNumber !== null && ` · #${item.issueNumber}`}
        </Typography>
        <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
          {item.title}
        </Typography>
        {item.reason === null && (
          <Typography component="span" variant="caption" color="text.secondary">
            {item.note}
          </Typography>
        )}
      </Box>
      <Chip
        size="small"
        variant="outlined"
        color={item.needsPerson ? "warning" : "default"}
        label={item.reason === null ? (item.needsPerson ? "Needs you" : "Incident report") : item.note}
      />
    </>
  );
  // A report filed without an issue has nowhere of its own to open.
  if (item.issueNumber === null) return <Box sx={ROW_SX}>{content}</Box>;
  return (
    <RowLink
      to="/projects/$projectName/issues/$number"
      params={{ projectName: item.projectName, number: String(item.issueNumber) }}
      sx={{ ...ROW_SX, "&:hover": { bgcolor: "action.hover" } }}
    >
      {content}
    </RowLink>
  );
}

function Alerts() {
  const alerts = useAlerts();
  if (alerts.error) return <Alert severity="error">{alerts.error}</Alert>;
  if (!alerts.items) return <Skeleton variant="rounded" height={64} />;
  const unread = alerts.unreadProjects.length;
  return (
    <>
      {unread > 0 && (
        <Alert severity="warning">
          The issues of {unread === 1 ? alerts.unreadProjects[0] : `${unread} projects`} could not be read, so what they need is
          not shown.
        </Alert>
      )}
      {alerts.items.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          Nothing needs you right now.
        </Typography>
      ) : (
        <Box component="nav" aria-label="Alerts" sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
          {alerts.items.map((item) => (
            <AlertRow key={item.key} item={item} />
          ))}
        </Box>
      )}
    </>
  );
}

/**
 * The user's home Page (`/`), opened from the rail's logo. It holds the
 * Alerts: the issues, across all projects, that need attention, those that
 * need a person first, each opening its Issue card; and any report the RCA
 * agent filed. It is also the base the org's Settings card is drawn over
 * (`routes/_dashboard/`).
 */
export function Dashboard() {
  return (
    <Box sx={{ maxWidth: 880, display: "flex", flexDirection: "column", gap: 2.5 }}>
      <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
        Dashboard
      </Typography>
      <Box component="section" aria-labelledby="alerts-heading" sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Typography id="alerts-heading" component="h2" sx={{ fontSize: "0.8125rem", fontWeight: 600 }}>
          Alerts
        </Typography>
        <Alerts />
      </Box>
    </Box>
  );
}
