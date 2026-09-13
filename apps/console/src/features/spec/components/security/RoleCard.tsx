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

/**
 * One role, as a card: what it is for, which PRD stories it serves, who signs
 * in as it, who may hand it out, and which org groups carry it.
 *
 * The card says nothing about WHICH handles the role grants — the matrix above
 * is where grants are read and compared, and repeating them here would give a
 * reader two places to check and one of them would eventually be stale.
 *
 * The group line is the design's aggregation: a group the directory already
 * holds is SHOWN with how many projects bind a role to it, not hidden. That
 * count is the thing a designer needs before reusing `Finance` — it is the
 * blast radius of the grant they are about to make.
 */

import {
  Box,
  Chip,
  Divider,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";

import type { SecurityReferenceFinding } from "@aep/agent-stream";

import type { ProjectRolesLiveState } from "../../api/roles";
import { plannedUsersFor, type SecurityDesign } from "../../api/securityDesign";
import { FindingLines } from "./FindingLine";

type Role = SecurityDesign["roles"][number];

/** One line saying how a person comes to hold this role. */
function enrolmentLine(role: Role): string {
  if ((role.kind ?? "user") === "service") {
    return "Held by a service, not by a person.";
  }
  if (role.enrolment === "self-service") {
    return "Self-service — the application assigns it when an account is created.";
  }
  const groups = role.assignTo ?? [];
  return groups.length > 0
    ? `Assigned to everyone in ${groups.join(", ")}.`
    : "Assigned by an administrator.";
}

/**
 * One directory chip: an `assignTo` GROUP of this role, judged against the
 * catalog the BFF returns.
 *
 * The live half is a group catalog, never a role catalog — a project role is
 * not an object on the directory, it reaches the app through the groups it is
 * assigned to. So the chip is per assignTo group, and a role with no assignTo
 * (a service role, a self-service one) gets none: there is nothing about it for
 * the directory to already hold.
 */
interface GroupStatus {
  group: string;
  label: string;
  color: "info" | "success" | "warning";
  why: string;
  /**
   * The design's "holds roles in n projects", or null when the count does not
   * apply (a group the directory does not have yet) or the BFF did not send one.
   */
  reach: string | null;
}

function groupStatuses(
  role: Role,
  live: ProjectRolesLiveState | undefined,
): GroupStatus[] {
  if (!live?.directoryAvailable) return [];
  // The platform's own record of THIS role, which carries the reach per
  // assignment. Absent before the first Build; the group catalog's own count is
  // then the best answer there is.
  const owned = live.projectRoles.find(
    (r) => r.name.toLowerCase() === role.name.toLowerCase(),
  );
  return (role.assignTo ?? []).map((group) => {
    const liveGroup = live.roles.find(
      (r) => r.name.toLowerCase() === group.toLowerCase(),
    );
    const members =
      (liveGroup?.memberCount ?? 0) > 0
        ? ` ${liveGroup?.memberCount} ${liveGroup?.memberCount === 1 ? "member" : "members"} today.`
        : "";
    if (!liveGroup) {
      return {
        group,
        label: "New at Build",
        color: "info" as const,
        why: `${group} does not exist on the identity provider yet — Build creates it.`,
        reach: null,
      };
    }
    const reach = projectReach(
      owned?.assignedTo?.find(
        (a) => a.group.toLowerCase() === group.toLowerCase(),
      )?.projects ?? liveGroup.projects,
    );
    if (liveGroup.platformCreated) {
      return {
        group,
        label: "Reused",
        color: "success" as const,
        why: `${group} is already on the identity provider, created by the platform.${members}`,
        reach,
      };
    }
    return {
      group,
      label: "Not ours",
      color: "warning" as const,
      why: `This group already exists and the platform did not create it, so it will be left alone.${members}`,
      reach,
    };
  });
}

/**
 * How far a group already reaches. Absent rather than "0 projects": the BFF
 * omits the field when it could not count, and a confident zero would read as
 * "nobody uses this" when the truth is "we do not know".
 */
function projectReach(projects: number | undefined): string | null {
  if (projects === undefined || projects <= 0) return null;
  return `holds roles in ${projects} ${projects === 1 ? "project" : "projects"}`;
}

export function RoleCard({
  doc,
  role,
  live,
  findings,
}: {
  doc: SecurityDesign;
  role: Role;
  live: ProjectRolesLiveState | undefined;
  findings: readonly SecurityReferenceFinding[];
}) {
  const statuses = groupStatuses(role, live);
  const planned = plannedUsersFor(doc, role.name);

  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 0.5 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
          {role.name}
        </Typography>
        {(role.kind ?? "user") === "service" && (
          <Chip size="small" variant="outlined" label="Service" />
        )}
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 0.5 }}>
        {role.description}
      </Typography>
      {role.stories.length > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
          Serves {role.stories.length === 1 ? "story" : "stories"}{" "}
          {role.stories.join(", ")}.
        </Typography>
      )}
      <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
        {enrolmentLine(role)}
      </Typography>
      {role.assignableBy && role.assignableBy.length > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
          Handed out by {role.assignableBy.join(", ")}.
        </Typography>
      )}

      <FindingLines findings={findings} />

      {statuses.length > 0 && (
        <Stack spacing={0.5} sx={{ mt: 1.5 }}>
          {statuses.map((status) => (
            <Stack
              key={status.group}
              direction="row"
              spacing={1}
              alignItems="center"
            >
              <Tooltip title={status.why}>
                <Chip
                  size="small"
                  color={status.color}
                  label={`${status.group}: ${status.label}`}
                />
              </Tooltip>
              {status.reach && (
                <Typography variant="caption" color="text.secondary">
                  · {status.reach}
                </Typography>
              )}
            </Stack>
          ))}
        </Stack>
      )}

      {planned.length > 0 && (
        <>
          <Divider sx={{ mt: 1.5, mb: 1 }} />
          <Typography variant="overline" color="text.secondary">
            Test users
          </Typography>
          <Stack spacing={0.5} sx={{ mt: 0.5 }}>
            {planned.map((u) => (
              <Stack key={u.username} direction="row" spacing={1} alignItems="center">
                <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
                  {u.username}
                </Typography>
                {u.supplied && (
                  <Tooltip title="You didn't name a test user for this role, so the platform will use this name.">
                    <Chip size="small" variant="outlined" label="Platform-supplied" />
                  </Tooltip>
                )}
              </Stack>
            ))}
          </Stack>
        </>
      )}
    </Box>
  );
}
