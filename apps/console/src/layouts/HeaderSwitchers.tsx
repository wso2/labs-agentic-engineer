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

import { ComplexSelect } from "@wso2/oxygen-ui";
import { Building, FolderOpen } from "@wso2/oxygen-ui-icons-react";
import { useParams, useRouter, useRouterState } from "@tanstack/react-router";
import { useHasPermission } from "../auth/permissions";
import { useSession } from "../auth/SessionContext";
import { useOrganizations } from "../features/organizations/api/queries";
import { useProjectsList } from "../features/projects/api/queries";

// Header switchers (issue #91), matching the oxygen-ui sample's
// Header.Switchers + ComplexSelect pattern.

// Honest single-org switcher: the current org comes from the token (the BFF
// scopes every request to it — OrgScopedInput), and tokens are single-org,
// so other orgs render disabled until real switching lands (issue #92).
export function OrgSwitcher() {
  const { orgHandle } = useSession();
  const { data } = useOrganizations();

  const orgs = data?.items ?? [];
  const current = orgs.find((o) => o.name === orgHandle);
  // The token's org may be missing from the listing (mock mode, stale list):
  // still show it as the selected entry rather than an empty select.
  const entries = current
    ? orgs
    : [
        ...(orgHandle
          ? [{ name: orgHandle, displayName: orgHandle, uuid: orgHandle }]
          : []),
        ...orgs,
      ];
  if (entries.length === 0) return null;

  const label = (name?: string, displayName?: string) => displayName || name;
  const selected = current ?? entries[0];

  return (
    <ComplexSelect
      value={orgHandle ?? entries[0]?.name ?? ""}
      size="small"
      sx={{ minWidth: 180 }}
      renderValue={() => (
        <>
          <ComplexSelect.MenuItem.Icon>
            <Building />
          </ComplexSelect.MenuItem.Icon>
          <ComplexSelect.MenuItem.Text
            primary={label(selected?.name, selected?.displayName)}
          />
        </>
      )}
      label="Organization"
      labelAnchor="inside"
    >
      <ComplexSelect.ListHeader>Organizations</ComplexSelect.ListHeader>
      {entries.map((org) => {
        const isCurrent = org.name === (orgHandle ?? entries[0]?.name);
        return (
          <ComplexSelect.MenuItem
            key={org.uuid ?? org.name}
            value={org.name}
            disabled={!isCurrent}
          >
            <ComplexSelect.MenuItem.Icon>
              <Building />
            </ComplexSelect.MenuItem.Icon>
            <ComplexSelect.MenuItem.Text
              primary={label(org.name, org.displayName)}
              secondary={
                isCurrent ? undefined : "Switching requires re-login — coming soon"
              }
            />
          </ComplexSelect.MenuItem>
        );
      })}
    </ComplexSelect>
  );
}

// Project switcher: only inside /projects/$projectName/*; switching swaps
// the param and keeps the current sub-page (tasks → tasks, spec → spec).
export function ProjectSwitcher() {
  const params = useParams({ strict: false }) as { projectName?: string };
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const router = useRouter();
  const { data } = useProjectsList("", 50);
  const canSwitch = useHasPermission("ae:requirement-view");

  const currentName = params.projectName;
  if (!currentName) return null;

  const projects = data?.pages.flatMap((page) => page.items ?? []) ?? [];
  const known = projects.some((p) => p.name === currentName);

  const switchTo = (nextName: string) => {
    if (!canSwitch || !nextName || nextName === currentName) return;
    // Same sub-page, different project — the param is a single path segment,
    // so a prefix swap is exact.
    const next = pathname.replace(
      `/projects/${currentName}`,
      `/projects/${nextName}`,
    );
    router.history.push(next);
  };

  return (
    <ComplexSelect
      value={known ? currentName : ""}
      onChange={(e) => switchTo(String(e.target.value))}
      size="small"
      sx={{ minWidth: 180 }}
      renderValue={() => (
        <>
          <ComplexSelect.MenuItem.Icon>
            <FolderOpen />
          </ComplexSelect.MenuItem.Icon>
          <ComplexSelect.MenuItem.Text primary={currentName} />
        </>
      )}
      label="Project"
      labelAnchor="inside"
    >
      <ComplexSelect.ListHeader>Projects</ComplexSelect.ListHeader>
      {projects.map((project) => (
        /* onClick per item, like legacy AepLayout: ComplexSelect's wrapped
           MenuItem doesn't reliably surface Select onChange. */
        <ComplexSelect.MenuItem
          key={project.name}
          value={project.name}
          disabled={!canSwitch}
          onClick={() => switchTo(project.name)}
        >
          <ComplexSelect.MenuItem.Icon>
            <FolderOpen />
          </ComplexSelect.MenuItem.Icon>
          <ComplexSelect.MenuItem.Text
            primary={project.displayName || project.name}
            secondary={
              !canSwitch
                ? "You don't have permission to open projects"
                : project.displayName && project.displayName !== project.name
                  ? project.name
                  : undefined
            }
          />
        </ComplexSelect.MenuItem>
      ))}
    </ComplexSelect>
  );
}
