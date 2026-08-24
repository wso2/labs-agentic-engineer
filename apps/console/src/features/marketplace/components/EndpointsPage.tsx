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
} from "@wso2/oxygen-ui";
import { Radio } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { useOrgEndpoints } from "../api/queries";

export function EndpointsPage() {
  const navigate = useNavigate();
  const { data, isPending, isError, error, refetch } = useOrgEndpoints();
  const items = data ?? [];

  return (
    <PageContent>
      <PageHeader
        title="Endpoints"
        subtitle="Marketplace Endpoints other projects offer. Offering a new API happens inside a project."
      />

      {isPending ? (
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading endpoints" />
        </Box>
      ) : isError ? (
        <Alert
          severity="error"
          action={<Button onClick={() => void refetch()}>Retry</Button>}
        >
          {error instanceof Error && error.message
            ? error.message
            : "Failed to load endpoints"}
        </Alert>
      ) : items.length === 0 ? (
        <EmptyState
          icon={<Radio size={48} />}
          title="No Marketplace Endpoints yet"
          description="Offering a new API happens inside a project. When a component is published as an org-service, it appears here."
        />
      ) : (
        <ListingTable.Container sx={{ width: "100%" }} disablePaper>
          <ListingTable variant="card" density="standard">
            <ListingTable.Body>
              {items.map((row) => (
                <ListingTable.Row
                  key={`${row.project}:${row.name}:${row.endpoint}`}
                  variant="card"
                  hover
                  clickable
                  onClick={() =>
                    void navigate({
                      to: "/projects/$projectName",
                      params: { projectName: row.project },
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
                            {row.name}
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            {row.type}
                          </Typography>
                        </Box>
                        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                          {row.project}
                        </Typography>
                        <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5 }}>
                          {row.endpoint}
                        </Typography>
                      </Box>
                    </Box>
                  </ListingTable.Cell>
                </ListingTable.Row>
              ))}
            </ListingTable.Body>
          </ListingTable>
        </ListingTable.Container>
      )}
    </PageContent>
  );
}
