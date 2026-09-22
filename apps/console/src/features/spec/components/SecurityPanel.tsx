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
 * Spec → Security: one scroll over `security.json` v3, in two sections.
 *
 * **Groups, roles & users** comes first — the people: every org group this
 * design touches, then a card per role. **Permissions** is the matrix, every
 * handle the project declares against every role a person can hold.
 *
 * The cast before the grid. A reader meeting the matrix first meets a column
 * header per role and no way to learn what any of them is for; meeting the
 * roles first, the columns are already names they know.
 *
 * Groups and roles were two sections and are now one. The split meant a group
 * and the role that uses it were never on screen together, and a REUSED group
 * had no row at all — it existed only as a chip inside whichever role card
 * named it. The standing prose each section carried (how the identity provider
 * works, what a test account is for) is gone from the page body: the first is
 * said per group by its New / Existing chip, the second hangs on the ⓘ beside
 * the test user it is about.
 *
 * It is the console's first WRITE into a spec room. One cell is one edit: the
 * room's CURRENT text is patched (`patchGrants`) inside the write itself, and
 * the room echoes the result back as a new `securityJson`. Nothing is held
 * optimistically — there is no second copy of the truth to reconcile, and a
 * write that the room refuses simply never arrives.
 *
 * Everything else on the page stays read-only. Adding a resource, an action or
 * a role touches `openapi.yaml` and `prototype.json` too, so it is a design
 * conversation; the matrix says so in its intro, ABOVE the grid — a reader has
 * to know it before they go hunting for a control that is not there, not after.
 */

import { useCallback, useMemo, useState, type ReactNode } from "react";
import {
  Alert,
  AlertTitle,
  Box,
  CircularProgress,
  IconButton,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Info } from "@wso2/oxygen-ui-icons-react";

import {
  prdActors,
  PRD_PATH,
  securityReferenceFindings,
  type SecurityReferenceContext,
} from "@aep/agent-stream";

import type { components } from "../../../generated/aep-api";
import { componentsWithoutSignIn } from "../../projects/lib/signInlessComponents";
import { resourceServerOf, type ProjectRolesLiveState } from "../api/roles";
import {
  parseSecurityDesign,
  securityMatrix,
  type SecurityDesign,
} from "../api/securityDesign";
import { patchGrants, type PatchFailure } from "../lib/patchGrants";
import { routeFindings } from "../lib/securityFindings";
import {
  GroupsBlock,
  IdentityIntro,
  SubLabel,
} from "./security/DocumentSections";
import { PermissionMatrix } from "./security/PermissionMatrix";
import { RoleCard } from "./security/RoleCard";

/** Why the cells are read-only when the room is not holding this file. */
const NO_ROOM_REASON =
  "Grants are edited in the live document. This page is showing the last committed copy, so cells are read-only until the collaborative room has the file.";

export interface SecurityPanelProps {
  /**
   * The project's handle — which is also its resource server's NAME on the
   * directory (the ensure creates it under exactly this string). Taken as a
   * prop rather than cut out of the identifier: the identifier's shape is the
   * platform's to change, and a console that parses it would break silently.
   */
  projectName: string;
  /** Live `security.json` text — from the room, or the committed fallback. */
  securityJson: string | null;
  live?: ProjectRolesLiveState | undefined;
  /** Committed-blob read in flight — same spinner as Architecture. */
  isPending?: boolean;
  /** Committed-blob read failed. */
  isError?: boolean;
  /**
   * The sibling spec files, for the rules that are about this document AND
   * another one. Absent is normal mid-design: those rules are then skipped in
   * silence rather than reported as failures.
   */
  references?: SecurityReferenceContext | undefined;
  /** True when the collab room holds this file and can take a write. */
  roomLive?: boolean;
  /**
   * Edit the document in the room. The updater is handed the room's text AS IT
   * IS NOW and returns the next whole document, or `null` to write nothing —
   * so the patch is computed against the text the diff is applied to, never
   * against the text of a render the agent has since overtaken.
   */
  writeSecurityJson?:
    | ((update: (current: string) => string | null) => void)
    | undefined;
  /**
   * Every component's declared dependencies, from the Spec view's own
   * design-dependencies read. `undefined` while that read is in flight or after
   * it failed — the "No sign-in at all" row is then omitted, exactly as it is
   * when every component provisions sign-in: the row exists only to name an
   * exposure, never to say there is none.
   */
  dependencies?: ComponentDependencies[] | undefined;
}

type ComponentDependencies = components["schemas"]["ComponentDependencies"];

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
  projectName,
  securityJson,
  live,
  isPending = false,
  isError = false,
  references,
  roomLive = false,
  writeSecurityJson,
  dependencies,
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
  // A document that does not parse costs the reader the WARNINGS too, and those
  // are the half a reader cannot see for themselves — so both states say so,
  // and say which one it is: waiting is not the same as broken.
  if (parsed.kind === "unfinished") {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="info">
          <AlertTitle>This Security document is still being written</AlertTitle>
          It stops mid-way, so there is nothing to show yet — including the
          warnings this page raises about it. They appear as soon as the design
          agent finishes writing.
        </Alert>
      </Box>
    );
  }
  if (parsed.kind === "invalid") {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">
          <AlertTitle>Couldn&apos;t read the Security document</AlertTitle>
          <Typography variant="body2" sx={{ mb: 1 }}>
            {parsed.message}
          </Typography>
          <Typography variant="body2">
            Nothing on this page was checked — the warnings it raises are worked
            out from the document, so one it cannot read gets none. Ask in chat
            to have it fixed.
          </Typography>
        </Alert>
      </Box>
    );
  }

  return (
    <SecurityDocument
      projectName={projectName}
      doc={parsed.doc}
      text={securityJson ?? ""}
      live={live}
      references={references}
      roomLive={roomLive}
      writeSecurityJson={writeSecurityJson}
      dependencies={dependencies}
    />
  );
}

/**
 * The page once the document reads. Split from `SecurityPanel` so the hooks
 * below run against a document that exists — the parse decides between five
 * renderings, and a hook cannot live behind that decision.
 */
function SecurityDocument({
  projectName,
  doc,
  text,
  live,
  references,
  roomLive,
  writeSecurityJson,
  dependencies,
}: {
  projectName: string;
  doc: SecurityDesign;
  text: string;
  live: ProjectRolesLiveState | undefined;
  references: SecurityReferenceContext | undefined;
  roomLive: boolean;
  writeSecurityJson:
    | ((update: (current: string) => string | null) => void)
    | undefined;
  dependencies: ComponentDependencies[] | undefined;
}) {
  // The failure is stamped with the document it was raised against, so it
  // clears itself the moment the page catches up: a banner saying the document
  // could not be read is about ONE text, and leaving it on screen after the
  // room delivered a newer one reports a problem that is no longer there.
  const [failure, setFailure] = useState<{
    at: string;
    failure: PatchFailure;
  } | null>(null);
  if (failure !== null && failure.at !== text) setFailure(null);

  const matrix = useMemo(() => securityMatrix(doc), [doc]);
  // A presentation filter, not a rule change. The rules are untouched and the
  // gate still raises every `info` — the design agent does benefit from being
  // told an assignment is legal. But an `info` is a note saying nothing is
  // wrong, written in the gate's vocabulary (`groups[]`, `list_groups`), and
  // the one this document raises — `assign_to_directory_checked` — is already
  // rendered better and from the LIVE directory as the role card's group chip.
  const findings = useMemo(
    () =>
      securityReferenceFindings(doc, references).filter(
        (finding) => finding.severity !== "info",
      ),
    [doc, references],
  );
  const routed = useMemo(() => {
    const rows = new Set(
      matrix.groups.flatMap((group) => group.rows.map((row) => row.handle)),
    );
    return routeFindings(findings, rows);
  }, [findings, matrix]);

  // The PRD's actors, for the line above the role cards. Read through the same
  // sibling-file reader every cross-check uses, so it follows the room; an
  // unreadable or absent PRD answers `[]` and the line is simply not drawn.
  const actors = useMemo(
    () => prdActors(references?.read(PRD_PATH) ?? ""),
    [references],
  );

  // Derived from the architecture, never authored — see `signInlessComponents`
  // for why that is the only honest source. An unanswered dependency read is
  // an empty list: the row then simply does not render, and nothing is claimed.
  const baseline = useMemo(
    () => ({
      openComponents: dependencies
        ? componentsWithoutSignIn(dependencies).map(componentLine)
        : [],
    }),
    [dependencies],
  );

  // Editability is asked ONCE. `writer` is the writer only when the room can
  // actually take the write, so the cells and the handler cannot disagree
  // about whether a click does anything.
  const writer = roomLive ? writeSecurityJson : undefined;

  const onToggleGrant = useCallback(
    (role: string, handle: string, next: boolean) => {
      if (!writer) return;
      // The patch runs INSIDE the write, on the room's current text — see
      // `useSecurityEntry`'s `writeSecurityJson` for why the rendered copy is
      // not good enough.
      writer((current) => {
        const result = patchGrants(current, role, handle, next);
        if (!result.ok) {
          setFailure({ at: text, failure: result.failure });
          return null;
        }
        setFailure(null);
        return result.text;
      });
    },
    [text, writer],
  );

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
          <ResourceServerLine projectName={projectName} live={live} />
        </Box>

        {failure && <PatchFailureAlert failure={failure.failure} />}

        <Box>
          <IdentityIntro actors={actors} />
          <GroupsBlock doc={doc} live={live} />
          <Box sx={{ mt: 2 }}>
            <SubLabel>Roles</SubLabel>
            <Box
              sx={{
                mt: 0.5,
                display: "grid",
                // auto-fit, not a fixed count: the panel shares its width with
                // the agent chat, so the column count has to answer to the
                // space there is rather than to a breakpoint that cannot see
                // whether the chat is open.
                gridTemplateColumns: "repeat(auto-fit, minmax(300px, 1fr))",
                gap: 1.5,
                alignItems: "start",
              }}
            >
              {doc.roles.map((role) => (
                <RoleCard
                  key={role.name}
                  doc={doc}
                  role={role}
                  findings={routed.byRole.get(role.name.toLowerCase()) ?? []}
                />
              ))}
            </Box>
          </Box>
        </Box>

        <PermissionMatrix
          matrix={matrix}
          baseline={baseline}
          findings={routed}
          readOnlyReason={writer ? undefined : NO_ROOM_REASON}
          onToggleGrant={onToggleGrant}
        />
      </Stack>
    </Box>
  );
}

/**
 * The resource server every grant of this project's roles is on — the access
 * token's `aud`, the audience the gateway checks, and the `resource` a scoped
 * token is asked for. One project has exactly one, so the first role's answer
 * is the project's.
 *
 * The NAME is on the line and the identifier is on the ⓘ. The identifier is a
 * URI that never resolves, and printing it in full made the widest thing under
 * the page title a string nobody reads twice — but it is also the exact value
 * someone diagnosing a 401 needs, so it stays one hover away rather than gone.
 *
 * Whether the line renders at all is still the platform's answer, not the
 * console's: `resourceServerOf` returning nothing means there is no record of
 * this project yet, and a name with no identifier behind it would be a claim
 * the page cannot back.
 */
function ResourceServerLine({
  projectName,
  live,
}: {
  projectName: string;
  live: ProjectRolesLiveState | undefined;
}) {
  const resourceServer = resourceServerOf(live);
  if (!resourceServer) return null;
  return (
    <Stack direction="row" spacing={0.75} alignItems="center" sx={{ mt: 0.5 }}>
      <Typography variant="caption" color="text.secondary">
        Resource server
      </Typography>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ fontFamily: "monospace" }}
      >
        {projectName}
      </Typography>
      <Tooltip
        title={`The audience every access token for this project carries, and the value the gateway checks: ${resourceServer}`}
      >
        <IconButton
          size="small"
          aria-label="About the resource server"
          sx={{ p: 0.25 }}
        >
          <Info size={13} aria-hidden />
        </IconButton>
      </Tooltip>
    </Stack>
  );
}

/** One baseline component: `component booking-site`. */
function componentLine(component: string): string {
  return `component ${component}`;
}

/**
 * A toggle that could not be applied: the document and the rendered matrix
 * disagree (someone edited the room mid-click, or the text is not what it was
 * parsed as), or the edit would produce a document the schema refuses. Either
 * way the honest answer is to say so and leave the document alone rather than
 * write a guess.
 *
 * It is shown only while the document it was raised against is still the one on
 * screen — see the stamp on the state above.
 */
function PatchFailureAlert({ failure }: { failure: PatchFailure }) {
  return (
    <Alert severity="error">
      {failure.kind === "no-such-role"
        ? `Couldn't change that grant: the document no longer has a role called "${failure.role}". It may have been edited in chat — the page will catch up.`
        : failure.kind === "last-grant"
          ? `Couldn't clear that grant: every role must grant at least one permission, and that is all "${failure.role}" has left. Grant it something else first, or ask in chat to remove the role.`
          : `Couldn't change that grant: the document could not be read (${failure.message}).`}
    </Alert>
  );
}
