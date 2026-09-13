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
 * Spec → Security: one scroll over `security.json` v2.
 *
 * The page is the permission MATRIX first — every handle the project declares
 * against every role a person can hold — and then a card per role for the
 * things a grid cannot say: what the role is for, who signs in as it, and which
 * org groups carry it.
 *
 * It is the console's first WRITE into a spec room. One cell is one edit: the
 * room's CURRENT text is patched (`patchGrants`) inside the write itself, and
 * the room echoes the result back as a new `securityJson`. Nothing is held
 * optimistically — there is no second copy of the truth to reconcile, and a
 * write that the room refuses simply never arrives.
 *
 * Everything else on the page stays read-only. Adding a resource, an action or
 * a role touches `openapi.yaml` and `wireframes.dsl` too, so it is a design
 * conversation; the page says so where a reader would look for the control.
 */

import { useCallback, useMemo, useState, type ReactNode } from "react";
import {
  Alert,
  AlertTitle,
  Box,
  CircularProgress,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";

import {
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
import { baselineOperations } from "./security/baselineOperations";
import {
  GroupsBlock,
  RolesIntro,
  ScreensBlock,
} from "./security/DocumentSections";
import { patchGrants, type PatchFailure } from "./security/patchGrants";
import { PermissionMatrix } from "./security/PermissionMatrix";
import { RoleCard } from "./security/RoleCard";
import { routeFindings } from "./security/findings";

/** Why the cells are read-only when the room is not holding this file. */
const NO_ROOM_REASON =
  "Grants are edited in the live document. This page is showing the last committed copy, so cells are read-only until the collaborative room has the file.";

export interface SecurityPanelProps {
  /** Live `security.json` text — from the room, or the committed fallback. */
  securityJson: string | null;
  live?: ProjectRolesLiveState | undefined;
  /** Committed-blob read in flight — same spinner as Architecture / Wireframes. */
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
   * it failed — the "open to everyone" row is then omitted rather than claiming
   * that every component provisions sign-in.
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
  if (parsed.kind === "invalid") {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">
          Couldn&apos;t read the Security document: {parsed.message}
        </Alert>
      </Box>
    );
  }

  return (
    <SecurityDocument
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
  doc,
  text,
  live,
  references,
  roomLive,
  writeSecurityJson,
  dependencies,
}: {
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
  const findings = useMemo(
    () => securityReferenceFindings(doc, references),
    [doc, references],
  );
  const routed = useMemo(() => {
    const rows = new Set(
      matrix.groups.flatMap((group) => group.rows.map((row) => row.handle)),
    );
    return routeFindings(findings, rows);
  }, [findings, matrix]);

  const baseline = useMemo(() => {
    const operations = baselineOperations(
      matrix.groups.map((group) => group.component),
      references,
    );
    return {
      signedIn: [
        ...operations.signedIn,
        ...matrix.baseline.signedInScreens.map(screenLine),
      ],
      open: [
        ...operations.open,
        ...matrix.baseline.publicScreens.map(screenLine),
      ],
      // v1 listed these by hand in `publicComponents`; v2 derives them from the
      // architecture, because `thunder-app` is the only thing that provisions
      // sign-in and a component that does not depend on it has none to offer.
      openComponents: dependencies
        ? componentsWithoutSignIn(dependencies)
        : null,
    };
  }, [matrix, references, dependencies]);

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
          <ResourceServerLine live={live} />
        </Box>

        {failure && <PatchFailureAlert failure={failure.failure} />}

        <PermissionMatrix
          matrix={matrix}
          baseline={baseline}
          findings={routed}
          readOnlyReason={writer ? undefined : NO_ROOM_REASON}
          onToggleGrant={onToggleGrant}
        />

        <GroupsBlock doc={doc} />
        <RolesIntro />
        <DisposableWarning />
        {doc.roles.map((role) => (
          <RoleCard
            key={role.name}
            doc={doc}
            role={role}
            live={live}
            findings={routed.byRole.get(role.name.toLowerCase()) ?? []}
          />
        ))}
        <ScreensBlock doc={doc} />
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
 * It is read from the platform's record rather than derived here: the console
 * guessing a URL that the gateway then does not accept would be worse than
 * saying nothing, and before the first Build there is no record to read.
 */
function ResourceServerLine({
  live,
}: {
  live: ProjectRolesLiveState | undefined;
}) {
  const resourceServer = resourceServerOf(live);
  if (!resourceServer) return null;
  return (
    <Stack direction="row" spacing={1} alignItems="baseline" sx={{ mt: 0.5 }}>
      <Typography variant="caption" color="text.secondary">
        Resource server
      </Typography>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ fontFamily: "monospace" }}
      >
        {resourceServer}
      </Typography>
    </Stack>
  );
}

/** One baseline screen, as the design writes it: `screen Find a slot (booking-site)`. */
function screenLine(entry: { component: string; screen: string }): string {
  return `screen ${entry.screen} (${entry.component})`;
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
