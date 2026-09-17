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
 * The parts of the page the matrix does not draw: the heading that opens the
 * identity half, and the org groups under it.
 *
 * Groups and roles are ONE section. They were two — a card of declared groups
 * above a separate "Roles & users" heading — and the split cost the reader the
 * only question they have about a group, which is whether it already exists:
 * a reused group had no row at all, appearing solely as a chip inside whichever
 * role card named it. Now every group this design touches is one list, each
 * with a New / Existing chip, and the role cards below it name the group they
 * are held by.
 */

import { Box, Stack, Typography } from "@wso2/oxygen-ui";
import type { ReactNode } from "react";

import type { ProjectRolesLiveState } from "../../api/roles";
import type { SecurityDesign } from "../../api/securityDesign";
import { groupReachLine, groupRows } from "../../lib/groupRows";
import { GroupChip } from "./GroupChip";

/** A bordered list of rows, divided rather than boxed one card each. */
function RowList({ children }: { children: ReactNode }) {
  return (
    <Box
      sx={{
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        "& > *": { borderBottom: 1, borderColor: "divider", px: 1.5, py: 1 },
        "& > *:last-of-type": { borderBottom: 0 },
      }}
    >
      {children}
    </Box>
  );
}

/**
 * The quiet label over a sub-list inside a section. It is deliberately not a
 * heading: Groups and Roles are two halves of one section, and promoting them
 * would put four headings on a page with two sections.
 */
export function SubLabel({ children }: { children: ReactNode }) {
  return (
    <Typography
      variant="overline"
      color="text.secondary"
      sx={{ display: "block", letterSpacing: "0.1em" }}
    >
      {children}
    </Typography>
  );
}

/**
 * The heading the groups and role cards open under.
 *
 * It no longer carries the paragraph about the platform identity provider: the
 * New / Existing chip on each group says the same thing per group and says it
 * from the live directory, so the paragraph was a less accurate second copy.
 */
export function IdentityIntro({ actors }: { actors: readonly string[] }) {
  return (
    <Box>
      <Typography variant="h6" sx={{ fontWeight: 600, mb: 0.5 }}>
        Groups, roles &amp; users
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 0.5 }}>
        Who holds the permissions this project defines. A group is the
        organisation&apos;s and is shared across projects; a role is this
        project&apos;s and is rewritten to match this document at every Build.
      </Typography>
      <ActorsLine actors={actors} />
    </Box>
  );
}

/**
 * Every org group this design touches — the ones it introduces and the ones it
 * reuses, in one list, each judged against the live directory.
 *
 * Nothing renders when the design names no group at all: an API-only project
 * whose roles are all service-kind has no directory surface to describe.
 */
export function GroupsBlock({
  doc,
  live,
}: {
  doc: SecurityDesign;
  live: ProjectRolesLiveState | undefined;
}) {
  const rows = groupRows(doc, live);
  if (rows.length === 0) return null;
  return (
    <Box sx={{ mt: 2 }}>
      <SubLabel>Groups</SubLabel>
      <Box sx={{ mt: 0.5 }}>
        <RowList>
          {rows.map((row) => {
            const reach = groupReachLine(row);
            return (
              <Stack
                key={row.name}
                data-testid="group-row"
                direction="row"
                spacing={1}
                alignItems="baseline"
                flexWrap="wrap"
                useFlexGap
              >
                <Typography variant="body2" sx={{ fontWeight: 600 }}>
                  {row.name}
                </Typography>
                <GroupChip row={row} />
                {row.description && (
                  <Typography variant="body2" color="text.secondary">
                    {row.description}
                  </Typography>
                )}
                {reach && (
                  <Typography variant="caption" color="text.secondary">
                    {reach}
                  </Typography>
                )}
              </Stack>
            );
          })}
        </RowList>
      </Box>
    </Box>
  );
}

/**
 * The actors the PRD names, beside the roles built from them.
 *
 * Stated, never checked. "Every PRD actor gets a role" is a real rule, but it
 * is not one a name comparison can judge: a role may not take a name an org
 * group already owns, so the actor `Finance` legitimately becomes the role
 * `FinanceReviewer`. A machine verdict here would be wrong on exactly the
 * designs that followed the rule — so the page puts the two lists in front of
 * the reader, who can tell in a glance what a matcher cannot.
 *
 * Nothing renders when the PRD is unreadable or names nobody: an empty actor
 * list is "we could not read it", not "this design has no actors".
 */
function ActorsLine({ actors }: { actors: readonly string[] }) {
  if (actors.length === 0) return null;
  return (
    <Typography variant="body2" color="text.secondary">
      The PRD names {actors.length === 1 ? "one actor" : `${actors.length} actors`}:{" "}
      <Box component="span" sx={{ color: "text.primary" }}>
        {actors.join(", ")}
      </Box>
      . Every one of them should have a role below — by description, not by
      name: a role cannot take a name an org group already owns.
    </Typography>
  );
}
