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
  Box,
  Button,
  Chip,
  Divider,
  Drawer,
  IconButton,
  Link,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Pencil, X } from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import { useHasPermission } from "../../../auth/permissions";
import type { components } from "../../../generated/aep-api";
import {
  CollapsibleSection,
  ConfigKeysSection,
  ConsumersSection,
  DeleteResourceSection,
  EmptyNote,
  OutputsSection,
  ParametersSection,
} from "../../settings/components/resource-inspect-sections";
import { isRegisteredExternal } from "../kind";
import { encodePromoteTarget } from "../lib/promoteTarget";

type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type EnvValueCellDTO = components["schemas"]["EnvValueCellDTO"];
type ResourceDocPointerDTO = components["schemas"]["ResourceDocPointerDTO"];
type ResourceInstanceDTO = components["schemas"]["ResourceInstanceDTO"];

export type CatalogTypeDrawerProps = {
  open: boolean;
  onClose: () => void;
  /**
   * Names the organization already holds a record for. A project's row of
   * such a name cannot be promoted (the name is taken); the project reuses
   * the record instead.
   */
  recordNames?: string[];
} & (
  | { kind: "platform"; resource: PlatformResourceTypeDTO }
  | { kind: "external"; resource: ExternalResourceDTO }
  | { kind: null; resource: null }
);

// A project's own resource: its values live on the project's builds. The
// organization takes them over by promoting the resource — from here, not from
// the project (the person who curates the registry does the promoting).
const PROJECT_EXTERNAL_ENV_NOTE =
  "Environment values are the project's. Promote the resource to hold them for the organization.";
const NAME_ALREADY_A_RECORD_NOTE =
  "The organization already holds a record with this name. Have the project reuse it instead.";

function EnvCellsSection({ cells }: { cells: EnvValueCellDTO[] }) {
  return (
    <CollapsibleSection title="Environment values" itemCount={cells.length}>
      <Stack spacing={1.5}>
        {cells.map((cell) => (
          <Stack
            key={`${cell.environment}/${cell.key}`}
            direction="row"
            spacing={1}
            alignItems="center"
          >
            <Typography component="code" variant="body2">
              {cell.key}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {cell.environment}
            </Typography>
            <Chip
              size="small"
              variant="outlined"
              label={cell.status === "configured" ? "Configured" : "Unset"}
            />
          </Stack>
        ))}
      </Stack>
    </CollapsibleSection>
  );
}

function ResourceDocsSection({ docs }: { docs: ResourceDocPointerDTO[] }) {
  return (
    <CollapsibleSection title="Resource docs" itemCount={docs.length}>
      {docs.length === 0 ? (
        <EmptyNote />
      ) : (
        <Stack spacing={1.5}>
          {docs.map((doc) => (
            <Box key={`${doc.type}:${doc.url ?? doc.path ?? ""}`}>
              <Chip size="small" variant="outlined" label={doc.type} />
              {doc.url && (
                <Link
                  href={doc.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  variant="body2"
                  sx={{ display: "block", mt: 0.5 }}
                >
                  {doc.url}
                </Link>
              )}
              {doc.path && !doc.url && (
                <Typography component="code" variant="body2" sx={{ display: "block", mt: 0.5 }}>
                  {doc.path}
                </Typography>
              )}
            </Box>
          ))}
        </Stack>
      )}
    </CollapsibleSection>
  );
}

function InstancesSection({ instances }: { instances: ResourceInstanceDTO[] }) {
  return (
    <CollapsibleSection title="Instances" itemCount={instances.length}>
      <Stack spacing={1.5}>
        {instances.map((instance) => (
          <Box key={`${instance.project}/${instance.environment}`}>
            <Typography variant="body2">{instance.project}</Typography>
            <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
              {instance.environment}
            </Typography>
            <Chip size="small" variant="outlined" label={instance.status} sx={{ mt: 0.5 }} />
          </Box>
        ))}
      </Stack>
    </CollapsibleSection>
  );
}

function ExternalResourceBody({
  resource,
  recordNames,
  onClose,
}: {
  resource: ExternalResourceDTO;
  recordNames: string[];
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const config = resource.config ?? [];
  const consumers = resource.consumers ?? [];
  const docs = resource.resourceDocs ?? [];
  const instances = resource.instances ?? [];
  const registered = isRegisteredExternal(resource);
  const heldBy = resource.project;

  return (
    <Box sx={{ mt: 2 }}>
      {heldBy ? (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 2 }}>
          held by {heldBy}
        </Typography>
      ) : null}
      {resource.provider && (
        <Box sx={{ mb: 2 }}>
          <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
            Provider
          </Typography>
          <Typography variant="body2">{resource.provider}</Typography>
        </Box>
      )}
      {resource.contract && (
        // The one document a project copies when it reuses this resource. The
        // path is the org docs repo's, so it is a fact and not a link.
        <Box sx={{ mb: 2 }}>
          <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
            Contract
          </Typography>
          <Stack direction="row" spacing={1} alignItems="center">
            <Chip size="small" variant="outlined" label={resource.contract.type} />
            <Typography component="code" variant="body2">
              {resource.contract.path}
            </Typography>
          </Stack>
        </Box>
      )}
      {resource.consumptionInstructions && (
        <Box sx={{ mb: 2 }}>
          <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
            Consumption instructions
          </Typography>
          <Typography variant="body2">{resource.consumptionInstructions}</Typography>
        </Box>
      )}
      <ConfigKeysSection config={config} />
      {registered ? (
        <EnvCellsSection cells={resource.envCells ?? []} />
      ) : (
        <Typography variant="body2" color="text.secondary" sx={{ my: 2 }}>
          {PROJECT_EXTERNAL_ENV_NOTE}
        </Typography>
      )}
      {registered ? <ResourceDocsSection docs={docs} /> : null}
      <ConsumersSection consumers={consumers} />
      {instances.length > 0 && <InstancesSection instances={instances} />}
      {registered ? (
        <DeleteResourceSection resource={resource} consumers={consumers} onClose={onClose} />
      ) : heldBy ? (
        <>
          <Divider sx={{ my: 2 }} />
          {recordNames.includes(resource.name) ? (
            <Typography variant="body2" color="text.secondary">
              {NAME_ALREADY_A_RECORD_NOTE}
            </Typography>
          ) : (
            <Button
              variant="contained"
              onClick={() => {
                void navigate({
                  to: "/resources/register/form",
                  search: { promote: encodePromoteTarget({ project: heldBy, name: resource.name }) },
                });
              }}
            >
              Promote to organization
            </Button>
          )}
        </>
      ) : null}
    </Box>
  );
}

function PlatformResourceBody({ resource }: { resource: PlatformResourceTypeDTO }) {
  const parameters = resource.parameters ?? {};
  const outputs = resource.outputs ?? [];
  const consumers = resource.consumers ?? [];
  return (
    <Box sx={{ mt: 2 }}>
      <ParametersSection parameters={parameters} />
      <OutputsSection outputs={outputs} />
      <ConsumersSection consumers={consumers} />
    </Box>
  );
}

export function CatalogTypeDrawer(props: CatalogTypeDrawerProps) {
  const { resource, open, onClose } = props;
  const navigate = useNavigate();
  const hasResourceConfig = useHasPermission("ae:resource-config");
  const showEdit =
    props.kind === "external" && isRegisteredExternal(props.resource);

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      // Force an opaque surface — see ResourceDrawer / BuildDependencyDrawer
      // for why the theme's default `background.paper` is unusable here.
      slotProps={{
        paper: {
          sx: {
            bgcolor: "background.default",
            backgroundImage: "none",
            backdropFilter: "none",
          },
        },
      }}
    >
      <Box sx={{ width: 440, p: 3 }}>
        <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 2 }}>
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            {resource?.name}
          </Typography>
          {showEdit && (
            <Tooltip
              title={
                hasResourceConfig
                  ? "Edit"
                  : "You don't have permission to configure resources."
              }
            >
              <span>
                <IconButton
                  aria-label="Edit"
                  disabled={!hasResourceConfig}
                  onClick={() => {
                    void navigate({
                      to: "/resources/register/form",
                      search: { name: props.resource.name },
                    });
                  }}
                >
                  <Pencil size={18} />
                </IconButton>
              </span>
            </Tooltip>
          )}
          <IconButton aria-label="Close" onClick={onClose}>
            <X size={20} />
          </IconButton>
        </Stack>
        <Divider sx={{ mb: 2 }} />
        {resource?.description && (
          <Typography variant="body2" color="text.secondary">
            {resource.description}
          </Typography>
        )}
        {props.kind === "external" && (
          <ExternalResourceBody
            resource={props.resource}
            recordNames={props.recordNames ?? []}
            onClose={onClose}
          />
        )}
        {props.kind === "platform" && <PlatformResourceBody resource={props.resource} />}
      </Box>
    </Drawer>
  );
}
