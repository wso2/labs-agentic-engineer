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
 * One role, as a card in a grid: what it is for, which PRD stories it serves,
 * who may hand it out, which org groups carry it and who signs in as it.
 *
 * The card says nothing about WHICH handles the role grants — the matrix below
 * is where grants are read and compared, and repeating them here would give a
 * reader two places to check and one of them would eventually be stale. It says
 * nothing about whether a group already exists either: that is the Groups list
 * above, once per group, rather than once per role that names it.
 *
 * What the card is sized for is a grid cell. Four roles at full width were four
 * bands of whitespace and most of the page's scroll.
 */

import {
  Box,
  Chip,
  IconButton,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Info } from "@wso2/oxygen-ui-icons-react";

import type { SecurityReferenceFinding } from "@aep/agent-stream";

import { plannedUsersFor, type SecurityDesign } from "../../api/securityDesign";
import { FindingLines } from "./FindingLine";

type Role = SecurityDesign["roles"][number];

/**
 * The standing fact about test accounts, on the thing it is about.
 *
 * It used to be a bold heading and a paragraph above every role on every
 * project — true of a healthy project as much as a broken one, and therefore
 * read by nobody after the first time. On the hover of the label it describes,
 * it is there for the reader who is asking and invisible to the one who is not.
 * Nothing is dropped, least of all the last clause.
 */
const TEST_USER_NOTE =
  "Disposable accounts for agents, not for real people. Each role gets one so " +
  "the validation agent can sign in and check what the role can actually do. " +
  "Passwords are shown on Deploy once Build publishes them — never name a real " +
  "person here.";

/**
 * One line saying how a person comes to hold this role, or null when the
 * `Held by` chips below say it better — a group name in a chip beside the
 * label is the same sentence with less of it.
 */
function enrolmentLine(role: Role): string | null {
  if ((role.kind ?? "user") === "service") {
    return "Held by a service, not by a person.";
  }
  if (role.enrolment === "self-service") {
    return "Self-service — real accounts take it at registration; its test login is bound straight to the role.";
  }
  if ((role.assignTo ?? []).length > 0) return null;
  return "Assigned by an administrator.";
}

/** The org groups whose members hold this role — none for a service role. */
function heldByGroups(role: Role): string[] {
  if ((role.kind ?? "user") === "service") return [];
  if (role.enrolment === "self-service") return [];
  return role.assignTo ?? [];
}

export function RoleCard({
  doc,
  role,
  findings,
}: {
  doc: SecurityDesign;
  role: Role;
  findings: readonly SecurityReferenceFinding[];
}) {
  const planned = plannedUsersFor(doc, role.name);
  const groups = heldByGroups(role);
  const enrolment = enrolmentLine(role);

  return (
    <Box
      data-testid="role-card"
      sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}
    >
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 0.5 }}>
        {/*
          A card title, deliberately NOT a heading element. `subtitle1` maps to
          `<h6>` by default, which is the level the page's sections use — so a
          role would have announced itself as a peer of Permissions rather than
          as one item inside a section.
        */}
        <Typography variant="subtitle1" component="div" sx={{ fontWeight: 600 }}>
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
      {enrolment && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
          {enrolment}
        </Typography>
      )}
      {role.assignableBy && role.assignableBy.length > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
          Handed out by {role.assignableBy.join(", ")}.
        </Typography>
      )}

      <FindingLines findings={findings} />

      {groups.length > 0 && (
        <LabelledRow label="Held by">
          {groups.map((group) => (
            <Chip key={group} size="small" variant="outlined" label={group} />
          ))}
        </LabelledRow>
      )}

      {planned.length > 0 && (
        <LabelledRow
          label={planned.length === 1 ? "Test user" : "Test users"}
          note={TEST_USER_NOTE}
          noteLabel="About test users"
        >
          {planned.map((u) => (
            <Stack key={u.username} direction="row" spacing={0.5} alignItems="center">
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
        </LabelledRow>
      )}
    </Box>
  );
}

/**
 * A quiet label with its values beside it, and optionally the ⓘ that carries
 * what the page no longer says out loud.
 *
 * The note hangs on an `IconButton` rather than plain text because a tooltip a
 * reader cannot reach is a tooltip half the readers do not have: the button is
 * in the tab order and answers to a keyboard, which a styled span is not.
 */
function LabelledRow({
  label,
  note,
  noteLabel,
  children,
}: {
  label: string;
  note?: string;
  noteLabel?: string;
  children: React.ReactNode;
}) {
  return (
    <Stack
      direction="row"
      spacing={1}
      alignItems="center"
      flexWrap="wrap"
      useFlexGap
      sx={{ mt: 1 }}
    >
      <Stack direction="row" spacing={0.25} alignItems="center">
        <Typography variant="caption" color="text.secondary">
          {label}
        </Typography>
        {note && (
          <Tooltip title={note}>
            <IconButton size="small" aria-label={noteLabel} sx={{ p: 0.25 }}>
              <Info size={13} aria-hidden />
            </IconButton>
          </Tooltip>
        )}
      </Stack>
      {children}
    </Stack>
  );
}
