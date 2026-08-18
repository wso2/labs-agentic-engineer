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

import { useState } from "react";
import {
  Avatar,
  Box,
  Card,
  CardContent,
  Link as MuiLink,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Boxes, ExternalLink } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import type { components } from "../../../generated/aep-api";
import { useComponentEndpointUrl } from "../api/queries";
import { ComponentOpenApiDialog } from "./ComponentOpenApiDialog";

type Component = components["schemas"]["Component"];

// The component type is OpenChoreo's own ComponentType name, end-to-end.
const isWebApp = (c: Component) => c.type === "web-application";

// Component cards: one compact single-row card per component — avatar, name and
// description. Services open their OpenAPI contract on click (JWT-guarded, so
// via the authenticated dialog, not a raw link). Web-applications show Open
// app when list-deployments has resolved a public URL.
export function ComponentsList({
  projectName,
  items,
}: {
  projectName: string;
  items: Component[];
}) {
  const [contractComponent, setContractComponent] = useState<string | null>(
    null,
  );

  if (items.length === 0) {
    return (
      <EmptyState
        bordered
        icon={<Boxes size={28} />}
        title="No components yet"
        description="The published plan produces them — they appear here as agents build."
      />
    );
  }

  return (
    <>
      <Stack spacing={1.5}>
        {items.map((c) => {
          const initial = ((c.displayName ?? c.name).trim()[0] ?? "C").toUpperCase();
          const openable = !isWebApp(c);
          const card = (
            <Card
              key={c.name}
              variant="outlined"
              {...(openable
                ? {
                    onClick: () => setContractComponent(c.name),
                    sx: {
                      cursor: "pointer",
                      transition: "border-color 120ms, box-shadow 120ms",
                      "&:hover": { borderColor: "primary.main", boxShadow: 1 },
                    },
                  }
                : {})}
            >
              <CardContent sx={{ py: 1.5, "&:last-child": { pb: 1.5 } }}>
                <Stack direction="row" spacing={2} sx={{ alignItems: "center" }}>
                  <Avatar
                    variant="rounded"
                    sx={{
                      width: 36,
                      height: 36,
                      bgcolor: "action.hover",
                      color: "text.primary",
                    }}
                  >
                    {initial}
                  </Avatar>
                  <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                    <Typography sx={{ fontWeight: 600 }} noWrap>
                      {c.displayName ?? c.name}
                    </Typography>
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      noWrap
                      sx={{ display: "block" }}
                    >
                      {c.description ?? "—"}
                    </Typography>
                  </Box>
                  {isWebApp(c) && (
                    <WebAppOpenLink
                      projectName={projectName}
                      displayName={c.displayName ?? c.name}
                      componentName={c.name}
                    />
                  )}
                </Stack>
              </CardContent>
            </Card>
          );
          return openable ? (
            <Tooltip key={c.name} title="View API contract" placement="left">
              {card}
            </Tooltip>
          ) : (
            card
          );
        })}
      </Stack>
      <ComponentOpenApiDialog
        projectName={projectName}
        componentName={contractComponent}
        onClose={() => setContractComponent(null)}
      />
    </>
  );
}

// Public URL for a web-application row (#196 / #538): the hook already exists
// and Deployments reads the same list-deployments field. Overview never called
// it, so a Ready SPA had no Open link here even when endpointUrl was set.
function WebAppOpenLink({
  projectName,
  componentName,
  displayName,
}: {
  projectName: string;
  componentName: string;
  displayName: string;
}) {
  const { data: url } = useComponentEndpointUrl(projectName, componentName);
  if (!url) return null;
  return (
    <MuiLink
      href={url}
      target="_blank"
      rel="noreferrer"
      variant="body2"
      aria-label={`Open ${displayName}`}
      sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, flexShrink: 0 }}
    >
      Open app <ExternalLink size={14} />
    </MuiLink>
  );
}
