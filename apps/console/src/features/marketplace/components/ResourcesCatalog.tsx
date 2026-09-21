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
  Alert,
  Box,
  Button,
  Card,
  CardActionArea,
  CardContent,
  Chip,
  CircularProgress,
  Grid,
  PageContent,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Boxes, Plus } from "@wso2/oxygen-ui-icons-react";
import { createLink } from "@tanstack/react-router";
import { useHasPermission } from "../../../auth/permissions";
import { EmptyState } from "../../../components/EmptyState";
import { NoPermissionIllustration } from "../../../components/NoPermissionIllustration";
import { PageHeader } from "../../../components/PageHeader";
import type { components } from "../../../generated/aep-api";
import { useExternalResources, usePlatformResourceTypes } from "../../settings/api/queries";
import { isRegisteredExternal } from "../kind";
import { CatalogTypeDrawer } from "./CatalogTypeDrawer";

type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type ConsumerDTO = components["schemas"]["ConsumerDTO"];

type CatalogSelection =
  | { kind: "platform"; resource: PlatformResourceTypeDTO }
  | { kind: "external"; resource: ExternalResourceDTO };

const RegisterLink = createLink(Button);

function CatalogCard({
  name,
  provider,
  project,
  description,
  consumers,
  platform,
  onOpen,
}: {
  name: string;
  /** The concrete system an external resource is; platform types have none. */
  provider?: string | undefined;
  /** The project that holds a resource of its own; a record has none. */
  project?: string | undefined;
  description?: string | undefined;
  consumers?: ConsumerDTO[] | null | undefined;
  platform: boolean;
  onOpen: () => void;
}) {
  const usedBy = consumers?.length ?? 0;
  return (
    <Card variant="outlined" sx={{ height: "100%" }}>
      <CardActionArea
        sx={{ height: "100%", alignItems: "stretch" }}
        onClick={onOpen}
      >
        <CardContent sx={{ display: "flex", flexDirection: "column", height: "100%" }}>
          <Stack direction="row" spacing={1} alignItems="flex-start" sx={{ mb: 1 }}>
            <Typography variant="h6" sx={{ flexGrow: 1 }}>
              {name}
            </Typography>
            {platform && <Chip size="small" label="Platform" />}
          </Stack>
          {provider ? (
            // The name is the organization's word for the resource; the
            // provider is the system it actually is, and the two are often
            // different ("currency-service" / "Open Exchange Rates").
            <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.5 }}>
              {provider}
            </Typography>
          ) : null}
          {project ? (
            <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.5 }}>
              held by {project}
            </Typography>
          ) : null}
          <Box sx={{ flexGrow: 1, minHeight: 0 }}>
            {description ? (
              <Typography
                variant="body2"
                color="text.secondary"
                title={description}
                sx={{
                  display: "-webkit-box",
                  WebkitLineClamp: 2,
                  WebkitBoxOrient: "vertical",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  overflowWrap: "anywhere",
                  maxHeight: "2lh",
                }}
              >
                {description}
              </Typography>
            ) : null}
          </Box>
          {usedBy > 0 && (
            <Typography variant="caption" color="text.secondary" sx={{ mt: 1.5 }}>
              Used by {usedBy}
            </Typography>
          )}
        </CardContent>
      </CardActionArea>
    </Card>
  );
}

export function ResourcesCatalog() {
  const hasResourceConfig = useHasPermission("ae:resource-config");
  const hasResourceAccess = useHasPermission("ae:resource-view");
  const platform = usePlatformResourceTypes(hasResourceAccess);
  const external = useExternalResources(hasResourceAccess);
  const [selection, setSelection] = useState<CatalogSelection | null>(null);

  const platformItems = platform.data ?? [];
  const externalItems = external.data ?? [];
  // The organization's records sit with the platform types; a project's own
  // resources follow in their own section, each naming its project, so the
  // organization can take one over (Promote) from here.
  const records = externalItems.filter(isRegisteredExternal);
  const heldByProjects = externalItems.filter((r) => !isRegisteredExternal(r));

  let body;
  if (!hasResourceAccess) {
    // Checked before the loading/error states below: without either
    // permission there is nothing here to load — both queries never fire
    // (usePlatformResourceTypes/useExternalResources(hasResourceAccess)) —
    // and a direct-URL visit must never flash real catalog content.
    body = (
      <EmptyState
        icon={<NoPermissionIllustration size={120} />}
        title="No resources access"
        description="You don't have permission to view resources."
      />
    );
  } else if (platform.isLoading || external.isLoading) {
    body = (
      <Box sx={{ display: "flex", justifyContent: "center", py: 6 }}>
        <CircularProgress aria-label="Loading resources" />
      </Box>
    );
  } else if (platform.isError || external.isError) {
    const message =
      (platform.error instanceof Error && platform.error.message) ||
      (external.error instanceof Error && external.error.message) ||
      "Failed to load resources";
    body = (
      <Alert
        severity="error"
        action={
          <Button
            onClick={() => {
              void platform.refetch();
              void external.refetch();
            }}
          >
            Retry
          </Button>
        }
      >
        {message}
      </Alert>
    );
  } else if (platformItems.length === 0 && externalItems.length === 0) {
    body = (
      <EmptyState
        icon={<Boxes size={48} />}
        title="No resources"
        description="The catalog is empty. Platform types and third-party resources appear here once they exist."
      />
    );
  } else {
    body = (
      <Stack spacing={4}>
        <Grid container spacing={3}>
          {platformItems.map((resource) => (
            <Grid key={`platform:${resource.name}`} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
              <CatalogCard
                name={resource.name}
                description={resource.description}
                consumers={resource.consumers}
                platform
                onOpen={() => setSelection({ kind: "platform", resource })}
              />
            </Grid>
          ))}
          {records.map((resource) => (
            <Grid key={`external:${resource.name}`} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
              <CatalogCard
                name={resource.name}
                provider={resource.provider}
                description={resource.description}
                consumers={resource.consumers}
                platform={false}
                onOpen={() => setSelection({ kind: "external", resource })}
              />
            </Grid>
          ))}
        </Grid>
        {heldByProjects.length > 0 ? (
          <Box component="section" aria-labelledby="held-by-projects-heading">
            <Typography id="held-by-projects-heading" variant="h6" sx={{ mb: 0.5 }}>
              Held by projects
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              Resources a project defined for itself. Promote one to hold its values for
              the organization and let other projects reuse it.
            </Typography>
            <Grid container spacing={3}>
              {heldByProjects.map((resource) => (
                <Grid
                  key={`external:${resource.project ?? ""}/${resource.name}`}
                  size={{ xs: 12, sm: 6, md: 4, lg: 3 }}
                >
                  <CatalogCard
                    name={resource.name}
                    provider={resource.provider}
                    project={resource.project}
                    description={resource.description}
                    consumers={resource.consumers}
                    platform={false}
                    onOpen={() => setSelection({ kind: "external", resource })}
                  />
                </Grid>
              ))}
            </Grid>
          </Box>
        ) : null}
      </Stack>
    );
  }

  return (
    <PageContent>
      <PageHeader
        title="Resources"
        subtitle="Platform types and third-party resources in this organization."
        {...(hasResourceAccess && {
          actions: hasResourceConfig ? (
            <RegisterLink
              variant="contained"
              startIcon={<Plus size={20} />}
              to="/resources/register"
            >
              Register
            </RegisterLink>
          ) : (
            <Tooltip title="You don't have permission to register resources.">
              <span>
                <Button variant="contained" startIcon={<Plus size={20} />} disabled>
                  Register
                </Button>
              </span>
            </Tooltip>
          ),
        })}
      />
      {body}
      <CatalogTypeDrawer
        {...(selection ?? { kind: null, resource: null })}
        recordNames={records.map((r) => r.name)}
        open={selection !== null}
        onClose={() => setSelection(null)}
      />
    </PageContent>
  );
}
