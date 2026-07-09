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
  Avatar,
  Chip,
  Link as MuiLink,
  ListingTable,
  Typography,
} from "@wso2/oxygen-ui";
import { ExternalLink, FileCode } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import { env } from "../../../config/env";

type Component = components["schemas"]["Component"];

// The component type is OpenChoreo's own ComponentType name, end-to-end.
const isWebApp = (c: Component) => c.type === "web-application";

function componentLink(projectName: string, c: Component) {
  if (isWebApp(c)) {
    return c.endpointUrl ? (
      <MuiLink
        href={c.endpointUrl}
        target="_blank"
        rel="noreferrer"
        variant="body2"
        sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}
      >
        Open app <ExternalLink size={14} />
      </MuiLink>
    ) : (
      <Typography variant="caption" color="text.secondary">
        URL appears once deployed
      </Typography>
    );
  }
  // API/service rows link to the component's OpenAPI contract.
  return (
    <MuiLink
      href={`${env.apiBaseUrl}/api/v1/projects/${projectName}/components/${c.name}/openapi`}
      target="_blank"
      rel="noreferrer"
      variant="body2"
      sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}
    >
      API contract <FileCode size={14} />
    </MuiLink>
  );
}

// Card-variant listing per the oxygen-ui sample's ProjectOverview page:
// header row floats above, each component renders as a full-width row card.
export function ComponentsList({
  projectName,
  items,
}: {
  projectName: string;
  items: Component[];
}) {
  if (items.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ py: 3 }}>
        No components yet — the published design produces them, and they show
        up here as agents build.
      </Typography>
    );
  }

  return (
    <ListingTable.Container sx={{ width: "100%" }} disablePaper>
      <ListingTable variant="card" density="standard">
        <ListingTable.Head>
          <ListingTable.Row>
            <ListingTable.Cell>Name</ListingTable.Cell>
            <ListingTable.Cell>Description</ListingTable.Cell>
            <ListingTable.Cell sx={{ maxWidth: 140 }}>Type</ListingTable.Cell>
            <ListingTable.Cell sx={{ maxWidth: 200 }}>Link</ListingTable.Cell>
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {items.map((c) => (
            <ListingTable.Row key={c.name} variant="card" hover>
              <ListingTable.Cell>
                <ListingTable.CellIcon
                  icon={
                    <Avatar
                      sx={{
                        width: 28,
                        height: 28,
                        bgcolor: "action.hover",
                        color: "text.primary",
                      }}
                    >
                      {((c.displayName ?? c.name).trim()[0] ?? "C").toUpperCase()}
                    </Avatar>
                  }
                  primary={c.displayName ?? c.name}
                />
              </ListingTable.Cell>
              <ListingTable.Cell>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  {c.description ?? "—"}
                </Typography>
              </ListingTable.Cell>
              <ListingTable.Cell sx={{ maxWidth: 140 }}>
                <Chip
                  label={isWebApp(c) ? "Web app" : (c.type ?? "—")}
                  size="small"
                  variant="outlined"
                />
              </ListingTable.Cell>
              <ListingTable.Cell sx={{ maxWidth: 200 }}>
                {componentLink(projectName, c)}
              </ListingTable.Cell>
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}
