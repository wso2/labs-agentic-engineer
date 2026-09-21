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
  Alert,
  Box,
  Button,
  CircularProgress,
  ListingTable,
  PageContent,
  Typography,
  formatRelativeTime,
} from "@wso2/oxygen-ui";
import { BellOff } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import { useHasPermission } from "../../../auth/permissions";
import { EmptyState } from "../../../components/EmptyState";
import { NoPermissionIllustration } from "../../../components/NoPermissionIllustration";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import { useAlertsInfinite } from "../api/queries";
import { classificationLabel, classificationTone } from "../classification";

export function AlertsList() {
  const navigate = useNavigate();
  const hasObservabilityAccess = useHasPermission("ae:observability-view");
  const {
    data,
    isPending,
    isError,
    error,
    refetch,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
  } = useAlertsInfinite(undefined, hasObservabilityAccess);

  const items = data?.pages.flatMap((page) => page.items ?? []) ?? [];

  return (
    <PageContent>
      <PageHeader
        title="Alerts"
        subtitle="RCA reports from the OpenChoreo SRE-agent handoff, across every project."
      />

      {!hasObservabilityAccess ? (
        // Checked before the loading/error states below: without
        // ae:observability-view there is nothing here to load — the alerts
        // query itself never fires (useAlertsInfinite(undefined,
        // hasObservabilityAccess)) — and a direct-URL visit must never flash
        // real alert content before this check runs.
        <EmptyState
          icon={<NoPermissionIllustration size={120} />}
          title="No alerts access"
          description="You don't have permission to view alerts."
        />
      ) : isPending ? (
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading alerts" />
        </Box>
      ) : isError ? (
        <Alert
          severity="error"
          action={<Button onClick={() => void refetch()}>Retry</Button>}
        >
          Failed to load alerts
          {error instanceof Error && error.message ? `: ${error.message}` : ""}
        </Alert>
      ) : items.length === 0 ? (
        <EmptyState
          icon={<BellOff size={48} />}
          title="No alerts yet"
          description="RCA reports from the SRE-agent handoff will show up here."
        />
      ) : (
        <>
          <ListingTable.Container sx={{ width: "100%" }} disablePaper>
            <ListingTable variant="card" density="standard">
              <ListingTable.Body>
                {items.map((report) => (
                  <ListingTable.Row
                    key={report.id}
                    variant="card"
                    hover
                    clickable
                    onClick={() =>
                      void navigate({
                        to: "/alerts/$alertId",
                        params: { alertId: report.id! },
                      })
                    }
                  >
                    <ListingTable.Cell>
                      <Box
                        display="flex"
                        alignItems="flex-start"
                        justifyContent="space-between"
                        gap={2}
                      >
                        <Box minWidth={0} flexGrow={1}>
                          <Box display="flex" alignItems="center" gap={1} flexWrap="wrap">
                            <Typography variant="subtitle1" sx={{ fontWeight: 700 }}>
                              {report.title}
                            </Typography>
                            {report.classification && (
                              <StatusChip
                                label={classificationLabel(report.classification)}
                                tone={classificationTone(report.classification)}
                                variant="outlined"
                              />
                            )}
                          </Box>
                          <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{
                              mt: 0.5,
                              display: "-webkit-box",
                              WebkitLineClamp: 2,
                              WebkitBoxOrient: "vertical",
                              overflow: "hidden",
                            }}
                          >
                            {report.summary}
                          </Typography>
                          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5 }}>
                            {report.project}
                          </Typography>
                        </Box>
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          sx={{ whiteSpace: "nowrap", flexShrink: 0 }}
                        >
                          {report.createdAt
                            ? formatRelativeTime(new Date(report.createdAt))
                            : ""}
                        </Typography>
                      </Box>
                    </ListingTable.Cell>
                  </ListingTable.Row>
                ))}
              </ListingTable.Body>
            </ListingTable>
          </ListingTable.Container>
          {hasNextPage && (
            <Box sx={{ display: "flex", justifyContent: "center", mt: 4 }}>
              <Button
                variant="outlined"
                onClick={() => void fetchNextPage()}
                loading={isFetchingNextPage}
              >
                Load more
              </Button>
            </Box>
          )}
        </>
      )}
    </PageContent>
  );
}
