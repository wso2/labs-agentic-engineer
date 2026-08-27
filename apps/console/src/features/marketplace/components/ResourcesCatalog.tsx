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
  Typography,
} from "@wso2/oxygen-ui";
import { Boxes, Plus } from "@wso2/oxygen-ui-icons-react";
import { createLink } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import type { components } from "../../../generated/aep-api";
import { useExternalResources, usePlatformResourceTypes } from "../../settings/api/queries";
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
  description,
  consumers,
  platform,
  onOpen,
}: {
  name: string;
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
  const platform = usePlatformResourceTypes();
  const external = useExternalResources();
  const [selection, setSelection] = useState<CatalogSelection | null>(null);

  const platformItems = platform.data ?? [];
  const externalItems = external.data ?? [];

  let body;
  if (platform.isLoading || external.isLoading) {
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
        {externalItems.map((resource) => (
          <Grid key={`external:${resource.name}`} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
            <CatalogCard
              name={resource.name}
              description={resource.description}
              consumers={resource.consumers}
              platform={false}
              onOpen={() => setSelection({ kind: "external", resource })}
            />
          </Grid>
        ))}
      </Grid>
    );
  }

  return (
    <PageContent>
      <PageHeader
        title="Resources"
        subtitle="Platform types and third-party resources in this organization."
        actions={
          <RegisterLink
            variant="contained"
            startIcon={<Plus size={20} />}
            to="/resources/register"
          >
            Register
          </RegisterLink>
        }
      />
      {body}
      <CatalogTypeDrawer
        {...(selection ?? { kind: null, resource: null })}
        open={selection !== null}
        onClose={() => setSelection(null)}
      />
    </PageContent>
  );
}
