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
 * Spec → Security: one scroll over `security.json` v2 — the permission
 * catalog, the roles that grant from it, the screens each handle opens, and
 * the test users. Read-only; the design agent writes the document in chat.
 *
 * This is the INTERIM v2 rendering: the same visual language the v1 panel had,
 * reading the fields v2 actually has. The permission matrix, the grant toggle
 * and the live cross-project counts are a later phase — nothing here writes.
 */

import { useMemo, type ReactNode } from "react";
import {
  Alert,
  AlertTitle,
  Box,
  Chip,
  CircularProgress,
  Divider,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";

import type { ProjectRolesLiveState } from "../api/roles";
import {
  parseSecurityDesign,
  plannedUsersFor,
  type SecurityDesign,
} from "../api/securityDesign";

export interface SecurityPanelProps {
  /** Live `security.json` text — from the room, or the committed fallback. */
  securityJson: string | null;
  live?: ProjectRolesLiveState | undefined;
  /** Committed-blob read in flight — same spinner as Architecture / Wireframes. */
  isPending?: boolean;
  /** Committed-blob read failed. */
  isError?: boolean;
}

function Centered({ children }: { children: ReactNode }) {
  return (
    <Box
      sx={{
        height: "100%",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      {children}
    </Box>
  );
}

export function SecurityPanel({
  securityJson,
  live,
  isPending = false,
  isError = false,
}: SecurityPanelProps) {
  const parsed = useMemo(() => parseSecurityDesign(securityJson), [securityJson]);

  if (isPending) {
    return (
      <Centered>
        <CircularProgress aria-label="Loading security" />
      </Centered>
    );
  }
  if (isError) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">Failed to load the Security document.</Alert>
      </Box>
    );
  }

  if (parsed.kind === "empty") {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="info">
          This Security document is empty or incomplete. Ask in chat — the design
          agent can finish it.
        </Alert>
      </Box>
    );
  }
  if (parsed.kind === "invalid") {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">
          Couldn&apos;t read the Security document: {parsed.message}
        </Alert>
      </Box>
    );
  }

  const { doc } = parsed;
  return (
    <Box sx={{ p: 3, overflow: "auto", height: "100%" }}>
      <Stack spacing={3}>
        <Box>
          <Typography variant="h5" sx={{ mb: 0.5 }}>
            Security
          </Typography>
          <Typography variant="body2" color="text.secondary">
            What this project protects, what each role may do with it, and the
            accounts the validation agent signs in with.
          </Typography>
        </Box>
        <PermissionsBlock doc={doc} />
        <GroupsBlock doc={doc} />
        <RolesIntro />
        <DisposableWarning />
        {doc.roles.map((role) => (
          <RoleCard key={role.name} doc={doc} role={role} live={live} />
        ))}
        <ScreensBlock doc={doc} />
      </Stack>
    </Box>
  );
}

function DisposableWarning() {
  return (
    <Alert severity="warning">
      <AlertTitle>
        Disposable accounts for agents, not for real people
      </AlertTitle>
      Each role gets a test user so the validation agent can sign in and check
      what that role can actually do. Usernames live here; passwords are shown
      on Deploy after Build publishes them — never name a real person.
    </Alert>
  );
}

/** What rows an action reaches, said in words. */
function ownershipLabel(ownership: SecurityDesign["permissions"][number]["actions"][number]["ownership"]) {
  return ownership === "own" ? "own rows" : "any row";
}

function PermissionsBlock({ doc }: { doc: SecurityDesign }) {
  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
        Permissions
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        Everything this project protects. A role grants these by name, and the
        API asks for them by name — nothing else is a permission.
      </Typography>
      <Stack spacing={1.5}>
        {doc.permissions.map((permission) => (
          <Box key={permission.resource}>
            <Stack direction="row" spacing={1} alignItems="baseline">
              <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
                {permission.resource}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                owned by {permission.component}
              </Typography>
            </Stack>
            {permission.description && (
              <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
                {permission.description}
              </Typography>
            )}
            <Stack spacing={0.25} sx={{ mt: 0.5 }}>
              {permission.actions.map((action) => (
                <Stack
                  key={action.handle}
                  direction="row"
                  spacing={1}
                  alignItems="center"
                >
                  <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
                    {permission.resource}:{action.handle}
                  </Typography>
                  <Chip
                    size="small"
                    variant="outlined"
                    label={ownershipLabel(action.ownership)}
                  />
                  {action.description && (
                    <Typography variant="body2" color="text.secondary">
                      {action.description}
                    </Typography>
                  )}
                </Stack>
              ))}
            </Stack>
          </Box>
        ))}
      </Stack>
    </Box>
  );
}

/**
 * Only the groups this project INTRODUCES. A role may be assigned to a group
 * the org already has; that one is named on the role, not declared here.
 */
function GroupsBlock({ doc }: { doc: SecurityDesign }) {
  if (doc.groups.length === 0) return null;
  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
        New org groups
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        Groups this project adds to the organisation directory at Build. They
        are shared — other projects can assign roles to them too.
      </Typography>
      <Stack spacing={0.5}>
        {doc.groups.map((group) => (
          <Typography key={group.name} variant="body2">
            <Box component="span" sx={{ fontWeight: 600 }}>
              {group.name}
            </Box>
            {" — "}
            {group.description}
          </Typography>
        ))}
      </Stack>
    </Box>
  );
}

function RolesIntro() {
  return (
    <Box>
      <Typography variant="h5" sx={{ mb: 0.5 }}>
        Roles &amp; users
      </Typography>
      <Typography variant="body2" color="text.secondary">
        These roles are created on the platform identity provider when you
        click Build — the same directory every project shares, so a role
        another project already uses is reused rather than duplicated.
      </Typography>
    </Box>
  );
}

/** One line saying how a person comes to hold this role. */
function enrolmentLine(role: SecurityDesign["roles"][number]): string {
  if (role.kind === "service") {
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
}

function groupStatuses(
  role: SecurityDesign["roles"][number],
  live: ProjectRolesLiveState | undefined,
): GroupStatus[] {
  if (!live?.directoryAvailable) return [];
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
      };
    }
    if (liveGroup.platformCreated) {
      return {
        group,
        label: "Reused",
        color: "success" as const,
        why: `${group} is already on the identity provider, created by the platform.${members}`,
      };
    }
    return {
      group,
      label: "Not ours",
      color: "warning" as const,
      why: `This group already exists and the platform did not create it, so it will be left alone.${members}`,
    };
  });
}

function RoleCard({
  doc,
  role,
  live,
}: {
  doc: SecurityDesign;
  role: SecurityDesign["roles"][number];
  live: ProjectRolesLiveState | undefined;
}) {
  const statuses = groupStatuses(role, live);
  const planned = plannedUsersFor(doc, role.name);
  const describeHandle = useHandleDescriptions(doc);

  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 0.5 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
          {role.name}
        </Typography>
        {role.kind === "service" && (
          <Chip size="small" variant="outlined" label="Service" />
        )}
        {statuses.map((status) => (
          <Tooltip key={status.group} title={status.why}>
            <Chip
              size="small"
              color={status.color}
              label={`${status.group}: ${status.label}`}
            />
          </Tooltip>
        ))}
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 0.5 }}>
        {role.description}
      </Typography>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ display: "block" }}
      >
        {enrolmentLine(role)}
      </Typography>
      {role.assignableBy && role.assignableBy.length > 0 && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: "block" }}
        >
          Handed out by {role.assignableBy.join(", ")}.
        </Typography>
      )}
      <Stack spacing={0.25} sx={{ mt: 1.5, mb: 1.5 }}>
        {role.grants.map((handle) => (
          <Stack key={handle} direction="row" spacing={1} alignItems="center">
            <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
              {handle}
            </Typography>
            {describeHandle(handle) && (
              <Typography variant="body2" color="text.secondary">
                {describeHandle(handle)}
              </Typography>
            )}
          </Stack>
        ))}
      </Stack>
      <Divider sx={{ mb: 1 }} />
      <Typography variant="overline" color="text.secondary">
        Test users
      </Typography>
      <Stack spacing={0.5} sx={{ mt: 0.5 }}>
        {planned.map((u) => (
          <Stack
            key={u.username}
            direction="row"
            spacing={1}
            alignItems="center"
          >
            <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
              {u.username}
            </Typography>
            {u.supplied && (
              <Tooltip title="You didn't name a test user for this role, so the platform will use this name.">
                <Chip
                  size="small"
                  variant="outlined"
                  label="Platform-supplied"
                />
              </Tooltip>
            )}
          </Stack>
        ))}
      </Stack>
    </Box>
  );
}

/**
 * A grant is a bare handle; the sentence beside it comes from the catalog entry
 * it names. Absent when the action carries no description — never invented.
 */
function useHandleDescriptions(doc: SecurityDesign) {
  const byHandle = useMemo(() => {
    const map = new Map<string, string>();
    for (const permission of doc.permissions) {
      for (const action of permission.actions) {
        if (action.description) {
          map.set(`${permission.resource}:${action.handle}`, action.description);
        }
      }
    }
    return map;
  }, [doc]);
  return (handle: string) => byHandle.get(handle);
}

/** What a caller must hold to reach each screen the wireframes declare. */
function ScreensBlock({ doc }: { doc: SecurityDesign }) {
  if (doc.screens.length === 0) return null;
  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
        Screens
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        What a person must hold to reach each screen.
      </Typography>
      <Stack spacing={0.5}>
        {doc.screens.map((screen) => (
          <Stack
            key={`${screen.component}:${screen.screen}`}
            direction="row"
            spacing={1}
            alignItems="center"
          >
            <Typography variant="body2">{screen.screen}</Typography>
            <Typography variant="caption" color="text.secondary">
              {screen.component}
            </Typography>
            {screen.requires === null ? (
              <Typography variant="body2" color="text.secondary">
                Any signed-in person
              </Typography>
            ) : screen.requires === "public" ? (
              <Typography variant="body2" color="text.secondary">
                Open to everyone, no sign-in
              </Typography>
            ) : (
              <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
                {screen.requires}
              </Typography>
            )}
          </Stack>
        ))}
      </Stack>
    </Box>
  );
}
