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
 * The permission matrix: every handle this project declares, against every role
 * a person can hold.
 *
 * Rows are the catalog grouped by resource, each group headed by the resource
 * and the component that owns it; columns are the user-kind roles; a filled
 * mark is a grant. Comparing roles is the reader's task, which is why this is
 * one grid rather than a card per role — the design settled that trade
 * explicitly, against "cards only" and "per-resource cards".
 *
 * Three rows sit under the grid rather than in it, because they cross no
 * column: what any signed-in caller reaches without holding a handle, what is
 * open before sign-in, and which components provision no sign-in at all. They
 * are the baseline the grid is read against, and the design puts them in the
 * same grid so the reader does not have to go and find them in the API view.
 *
 * The last two are adjacent and deliberately not merged. A public screen or
 * operation belongs to a component that DOES have sign-in and has chosen to
 * leave this one door open; a component with no sign-in dependency has no door
 * to close. A reader deciding whether this project exposes anything has to see
 * both, and has to be able to tell them apart.
 *
 * Service-kind roles are not columns. A service principal holds an application
 * token and reaches no screen, so a column beside the roles a person holds
 * would invite reading a login into a grant that has none; they get their own
 * list, with the principal they attach to.
 *
 * The model is `securityMatrix` — a pure fold shared with the tests, so the
 * shape of this page is asserted without React.
 */

import {
  Box,
  Chip,
  ListingTable,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";

import {
  isGranted,
  isLastGrant,
  type MatrixColumn,
  type MatrixResourceGroup,
  type Ownership,
  type SecurityMatrix,
} from "../../api/securityDesign";
import { FindingLines } from "./FindingLine";
import { GrantCell } from "./GrantCell";
import type { RoutedFindings } from "./findings";

/**
 * The rows under the catalog, already merged from the document's screens, the
 * component contracts' operations, and the architecture — one list per row, in
 * reading order.
 */
export interface BaselineRows {
  signedIn: string[];
  open: string[];
  /**
   * Components that declare no sign-in dependency at all, so whatever they
   * serve they serve to everyone. `null` means the dependency read has not
   * answered — a different fact from "every component needs sign-in", and the
   * row is omitted rather than asserting the wrong one.
   */
  openComponents: string[] | null;
}

export interface PermissionMatrixProps {
  matrix: SecurityMatrix;
  baseline: BaselineRows;
  findings: RoutedFindings;
  /** Why no cell can be toggled, or undefined when they all can. */
  readOnlyReason?: string | undefined;
  onToggleGrant: (role: string, handle: string, next: boolean) => void;
}

/**
 * Why a cell that IS granted still cannot be cleared: it is the role's only
 * grant, and a role with none is a document this page cannot read back. The
 * way out is a design conversation, which is where a role is removed anyway.
 */
function lastGrantReason(role: string): string {
  return `Every role must grant at least one permission, so this one cannot be cleared — it is all ${role} has. Grant ${role} something else first, or ask in chat to remove the role.`;
}

/** What rows an action reaches, said in words a reader does not have to decode. */
function ownershipWords(ownership: Ownership): string {
  return ownership === "own"
    ? "own — the caller's own records only"
    : "any — every record of this resource";
}

export function PermissionMatrix({
  matrix,
  baseline,
  findings,
  readOnlyReason,
  onToggleGrant,
}: PermissionMatrixProps) {
  // Handle + description + one per role: what a full-width row has to span.
  const span = 2 + matrix.columns.length;

  return (
    <Box>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
        Permissions
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        Everything this project protects, and which role may do it. A role
        grants these by name and the API asks for them by name — nothing else is
        a permission.
      </Typography>

      <FindingLines findings={findings.document} />

      <ListingTable.Container sx={{ width: "100%", mt: 1 }} disablePaper>
        <ListingTable density="compact" bordered>
          <ListingTable.Head>
            <ListingTable.Row>
              <ListingTable.Cell>Permission</ListingTable.Cell>
              <ListingTable.Cell />
              {matrix.columns.map((column) => (
                <ListingTable.Cell key={column.name} align="center">
                  {column.name}
                </ListingTable.Cell>
              ))}
            </ListingTable.Row>
          </ListingTable.Head>
          <ListingTable.Body>
            {matrix.groups.map((group) => (
              <ResourceGroup
                key={group.resource}
                group={group}
                columns={matrix.columns}
                findings={findings}
                readOnlyReason={readOnlyReason}
                onToggleGrant={onToggleGrant}
                span={span}
              />
            ))}
            <BaselineRow
              label="any signed-in user"
              entries={baseline.signedIn}
              span={span}
              empty="Nothing is reachable on a token alone."
            />
            <BaselineRow
              label="public"
              entries={baseline.open}
              span={span}
              empty="Nothing is open before sign-in."
            />
            {baseline.openComponents !== null && (
              <BaselineRow
                label="open to everyone"
                entries={baseline.openComponents}
                span={span}
                empty="Every component in this project provisions sign-in."
              />
            )}
          </ListingTable.Body>
        </ListingTable>
      </ListingTable.Container>

      <ServiceRoles columns={matrix.serviceColumns} />

      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ display: "block", mt: 1.5 }}
      >
        Adding a resource, an action or a role, or changing what an operation
        requires, is a design conversation — it touches the API contract and the
        wireframes too. Ask in chat and the design agent edits them together. A
        cell here only moves a grant between roles that already exist.
      </Typography>
    </Box>
  );
}

function ResourceGroup({
  group,
  columns,
  findings,
  readOnlyReason,
  onToggleGrant,
  span,
}: {
  group: MatrixResourceGroup;
  columns: MatrixColumn[];
  findings: RoutedFindings;
  readOnlyReason: string | undefined;
  onToggleGrant: (role: string, handle: string, next: boolean) => void;
  span: number;
}) {
  return (
    <>
      <ListingTable.Row>
        <ListingTable.Cell colSpan={span} sx={{ bgcolor: "action.hover" }}>
          <Stack direction="row" spacing={1} alignItems="baseline">
            <Typography
              variant="body2"
              sx={{ fontFamily: "monospace", fontWeight: 600 }}
            >
              {group.resource}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {group.component}
            </Typography>
            {group.description && (
              <Typography variant="caption" color="text.secondary">
                {group.description}
              </Typography>
            )}
          </Stack>
        </ListingTable.Cell>
      </ListingTable.Row>
      {group.rows.map((row) => {
        const rowFindings = findings.byHandle.get(row.handle) ?? [];
        return (
          <ListingTable.Row key={row.handle}>
            <ListingTable.Cell>
              <Stack direction="row" spacing={1} alignItems="center">
                <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
                  {row.handle}
                </Typography>
                <Tooltip title={ownershipWords(row.ownership)}>
                  <Chip size="small" variant="outlined" label={row.ownership} />
                </Tooltip>
              </Stack>
              <FindingLines findings={rowFindings} dense />
            </ListingTable.Cell>
            <ListingTable.Cell>
              {row.description && (
                <Typography variant="body2" color="text.secondary">
                  {row.description}
                </Typography>
              )}
            </ListingTable.Cell>
            {columns.map((column) => (
              <ListingTable.Cell key={column.name} align="center">
                <GrantCell
                  role={column.name}
                  handle={row.handle}
                  granted={isGranted(row, column)}
                  disabledReason={
                    readOnlyReason ??
                    (isLastGrant(row, column)
                      ? lastGrantReason(column.name)
                      : undefined)
                  }
                  onToggle={(next) => onToggleGrant(column.name, row.handle, next)}
                />
              </ListingTable.Cell>
            ))}
          </ListingTable.Row>
        );
      })}
    </>
  );
}

/**
 * One of the rows with no column. They are inside the same grid on purpose: the
 * design's point is that the reader sees the baseline where they read the
 * grants, not in a panel they have to remember to open.
 */
function BaselineRow({
  label,
  entries,
  span,
  empty,
}: {
  label: string;
  entries: string[];
  span: number;
  empty: string;
}) {
  return (
    <ListingTable.Row>
      <ListingTable.Cell>
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {label}
        </Typography>
      </ListingTable.Cell>
      <ListingTable.Cell colSpan={span - 1}>
        <Typography variant="body2" color="text.secondary">
          {entries.length > 0 ? entries.join(" · ") : empty}
        </Typography>
      </ListingTable.Cell>
    </ListingTable.Row>
  );
}

/**
 * The service-kind roles and what they hold. The principal each attaches to is
 * created with the project's service identity; until that exists the list says
 * so rather than printing a name nothing will answer to.
 */
function ServiceRoles({ columns }: { columns: MatrixColumn[] }) {
  if (columns.length === 0) return null;
  return (
    <Box sx={{ mt: 2 }}>
      <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
        Service principals
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        Held by an application, not by a person — no login, no group, no screen.
        The principal each attaches to arrives with the project&apos;s service
        identity; until then the job forwards no token at all.
      </Typography>
      <Stack spacing={0.5}>
        {columns.map((column) => (
          <Stack key={column.name} direction="row" spacing={1} alignItems="baseline">
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {column.name}
            </Typography>
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{ fontFamily: "monospace" }}
            >
              {column.role.grants.join(" ")}
            </Typography>
          </Stack>
        ))}
      </Stack>
    </Box>
  );
}
