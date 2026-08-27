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
 * The Security entry: one rail row, two tabs over the two halves of the security
 * design.
 *
 * **Security architecture** is `specs/design/security.md`, the prose half —
 * how a caller's role is resolved and which surfaces are public — handed in as a
 * node because the caller is the one place that knows how to build a live
 * collaborative editor. It opens first: the design decision comes before its
 * roster, and a reader arriving at Security wants the mechanism, not a table of
 * accounts. **Roles & users** renders `specs/design/roles.json` — which roles
 * the project uses and the test users that exercise them — and is where the user
 * names their own test users before Build.
 *
 * The prose keeps a pane to itself rather than sharing a scroll container with
 * the table: it is a Tiptap editor with a docked toolbar, a bubble menu and
 * streaming autoscroll, and form controls beside it fight all three.
 */

import { useMemo, useState } from "react";
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Chip,
  Divider,
  IconButton,
  Stack,
  Tab,
  Tabs,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import {
  Check,
  Copy,
  Eye,
  Plus,
  RotateCcwKey,
  Trash2,
  UserPen,
  X,
} from "@wso2/oxygen-ui-icons-react";

import type { ProjectRolesLiveState, ProjectTestUserState } from "../api/roles";
import {
  addTestUser,
  parseRolesDesign,
  plannedUsersFor,
  planUsers,
  removeTestUser,
  renameTestUser,
  suppliedUsernameFor,
  type RolesDesign,
} from "../api/rolesDesign";

export interface SecurityPanelProps {
  /** The live `roles.json` text — from the room, or the committed fallback. */
  rolesJson: string | null;
  /**
   * Apply an edited document. Undefined when nothing can be written (no room,
   * or a committed-only read), which renders the table read-only rather than
   * offering controls that would silently do nothing.
   */
  onRolesChange?: ((next: string) => void) | undefined;
  /** What the platform actually created. Undefined while it loads. */
  live?: ProjectRolesLiveState | undefined;
  /**
   * The account actions. Each is a deliberate act on a SHARED directory object,
   * so each is a callback the page owns rather than something this panel does
   * for itself. Absent ⇒ the controls are not rendered at all, which is the
   * honest rendering when nothing can act.
   */
  actions?: AccountActions | undefined;
  /** The prose half's editor. */
  prose: React.ReactNode;
}

export interface AccountActions {
  reveal: (username: string) => Promise<string>;
  rotate: (username: string) => Promise<string>;
  remove: (username: string) => Promise<void>;
}

/**
 * The parsed document plus everything needed to render and change it, bundled
 * because the four travelled together to every level of this panel and were
 * re-spelled at each hop. One type, one prop, and a component deeper in the tree
 * gains a capability without four signatures changing.
 */
interface RolesContext {
  doc: RolesDesign;
  live: ProjectRolesLiveState | undefined;
  /** Undefined ⇒ read-only: no edit control renders at all. */
  onRolesChange: ((next: string) => void) | undefined;
  /** Undefined ⇒ no account action renders at all. */
  actions: AccountActions | undefined;
}

type TabId = "roles" | "prose";

export function SecurityPanel({
  rolesJson,
  onRolesChange,
  live,
  actions,
  prose,
}: SecurityPanelProps) {
  const [tab, setTab] = useState<TabId>("prose");

  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      <Tabs
        value={tab}
        onChange={(_, next: TabId) => setTab(next)}
        sx={{
          borderBottom: 1,
          borderColor: "divider",
          px: 2,
          flex: "0 0 auto",
        }}
      >
        <Tab value="prose" label="Security architecture" />
        <Tab value="roles" label="Roles & users" />
      </Tabs>
      {tab === "roles" ? (
        <Box sx={{ flex: 1, minHeight: 0, overflow: "auto", p: 3 }}>
          <RolesTab
            rolesJson={rolesJson}
            onRolesChange={onRolesChange}
            live={live}
            actions={actions}
          />
        </Box>
      ) : (
        <Box
          sx={{
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          {prose}
        </Box>
      )}
    </Box>
  );
}

function RolesTab({
  rolesJson,
  onRolesChange,
  live,
  actions,
}: Pick<
  SecurityPanelProps,
  "rolesJson" | "onRolesChange" | "live" | "actions"
>) {
  const parsed = useMemo(() => parseRolesDesign(rolesJson), [rolesJson]);

  if (parsed.kind === "absent") {
    return (
      <Alert severity="info">
        This design declares no roles yet. Ask in chat for the roles this
        project needs — the design agent writes them, and the platform creates
        them when you click Build.
      </Alert>
    );
  }
  if (parsed.kind === "invalid") {
    return (
      <Alert severity="error">
        Couldn&apos;t read the roles document: {parsed.message}
      </Alert>
    );
  }

  const rc: RolesContext = {
    doc: parsed.doc,
    live,
    onRolesChange,
    actions,
  };
  const doc = rc.doc;
  return (
    <Stack spacing={3}>
      <Box>
        <Typography variant="h5" sx={{ mb: 0.5 }}>
          Roles &amp; users
        </Typography>
        <Typography variant="body2" color="text.secondary">
          These roles are created on the platform identity provider when you
          click Build — the same directory every project shares, so a role
          another project already uses is reused rather than duplicated. Each
          role gets a test user so the validation agent can sign in and check
          what that role can actually do.
        </Typography>
      </Box>

      {/* Stated once for the section, never per role or per row: it is one
          standing fact about every test user, and twenty copies of it would read
          as twenty separate problems. The naming field carries the same caution
          in one line, where the choice is actually made.

          It has to be said at all because the password is not a secret the user
          chose to share — the build writes each login onto the roles gate ticket
          so the validation agent can read it, which puts it in front of everyone
          with repository access. `warning` rather than `info` for that reason:
          it is a security caution with an action attached (never name a real
          person), not a neutral fact, and an info box is skimmed. */}
      <Alert severity="warning">
        <AlertTitle>
          Disposable accounts for agents, not for real people
        </AlertTitle>
        The validation agent signs in as each test user to judge role-gated
        acceptance criteria, so the platform generates the password at Build and
        publishes the username and password in that build&apos;s roles gate
        ticket — anyone who can read this project&apos;s repository can read
        them. Never name a real person&apos;s account here: the platform leaves
        an account it did not create untouched, so you would get a role with no
        working login rather than a password reset.
      </Alert>

      {live && !live.directoryAvailable && (
        <Alert severity="warning">
          The identity provider could not be reached, so nothing below shows
          what exists today — only what this design declares.
        </Alert>
      )}

      {doc.roles.map((role) => (
        <RoleCard key={role.name} rc={rc} roleName={role.name} />
      ))}

      {doc.coldStartRole !== null && (
        <Typography variant="body2" color="text.secondary">
          A person who has just signed in and been granted nothing holds{" "}
          <strong>{doc.coldStartRole}</strong>.
        </Typography>
      )}
      {doc.coldStartRole === null && (
        <Typography variant="body2" color="text.secondary">
          A person who has just signed in and been granted nothing reaches
          nothing.
        </Typography>
      )}
      {doc.publicComponents.length > 0 && (
        <Typography variant="body2" color="text.secondary">
          Open to everyone, no sign-in: {doc.publicComponents.join(", ")}.
        </Typography>
      )}
    </Stack>
  );
}

function RoleCard({ rc, roleName }: { rc: RolesContext; roleName: string }) {
  const { doc, live, onRolesChange } = rc;
  const role = doc.roles.find((r) => r.name === roleName);
  const [adding, setAdding] = useState(false);
  if (!role) return null;

  const liveRole = live?.roles.find(
    (r) => r.name.toLowerCase() === role.name.toLowerCase(),
  );
  // Three states, and they are genuinely different: a role the platform made and
  // may add members to; a role somebody else made, which it will not touch; and
  // one that does not exist yet.
  const status = !live?.directoryAvailable
    ? null
    : !liveRole
      ? {
          label: "New at Build",
          color: "info" as const,
          why: "This role does not exist yet — Build creates it.",
        }
      : liveRole.platformCreated
        ? {
            label: "Reused",
            color: "success" as const,
            why: "Already on the identity provider, created by the platform.",
          }
        : {
            label: "Not ours",
            color: "warning" as const,
            why:
              "This group already exists and the platform did not create it, so it will be left " +
              "alone — no test user is added to it. Use a role of your own if you need one.",
          };

  const planned = plannedUsersFor(doc, role.name);

  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 0.5 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
          {role.name}
        </Typography>
        {status && (
          <Tooltip title={status.why}>
            <Chip size="small" color={status.color} label={status.label} />
          </Tooltip>
        )}
        {(liveRole?.memberCount ?? 0) > 0 && (
          <Typography variant="caption" color="text.secondary">
            {liveRole?.memberCount}{" "}
            {liveRole?.memberCount === 1 ? "member" : "members"}
          </Typography>
        )}
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        {role.description}
      </Typography>

      <Stack spacing={0.25} sx={{ mb: 1.5 }}>
        {role.permissions.map((p, i) => (
          <Typography key={`${p.component}:${i}`} variant="body2">
            <Box component="span" sx={{ fontFamily: "monospace" }}>
              {p.component}
            </Box>
            {" — "}
            {[...(p.actions ?? []), ...(p.screens ?? [])].join(", ")}
          </Typography>
        ))}
      </Stack>
      <Divider sx={{ mb: 1 }} />

      <Typography variant="overline" color="text.secondary">
        Test users
      </Typography>
      <Stack spacing={0.5} sx={{ mt: 0.5 }}>
        {planned.map((u) => (
          <TestUserRow
            key={u.username}
            rc={rc}
            username={u.username}
            supplied={u.supplied}
          />
        ))}
        {adding ? (
          <UsernameField
            initial={suppliedUsernameFor(doc, role.name)}
            taken={takenUsernames(doc)}
            onCancel={() => setAdding(false)}
            onCommit={(name) => {
              onRolesChange?.(addTestUser(doc, role.name, name));
              setAdding(false);
            }}
          />
        ) : (
          onRolesChange && (
            <Box>
              <Button
                size="small"
                startIcon={<Plus size={14} />}
                onClick={() => setAdding(true)}
              >
                Add a test user
              </Button>
            </Box>
          )
        )}
      </Stack>
    </Box>
  );
}

function TestUserRow({
  rc,
  username,
  supplied,
}: {
  rc: RolesContext;
  username: string;
  supplied: boolean;
}) {
  const { doc, live, onRolesChange, actions } = rc;
  const [renaming, setRenaming] = useState(false);
  const liveUser = live?.testUsers.find((u) => u.username === username);

  if (renaming) {
    return (
      <UsernameField
        initial={username}
        taken={takenUsernames(doc).filter((n) => n !== username)}
        onCancel={() => setRenaming(false)}
        onCommit={(next) => {
          onRolesChange?.(renameTestUser(doc, username, next));
          setRenaming(false);
        }}
      />
    );
  }

  return (
    <Stack direction="row" spacing={1} alignItems="center">
      <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
        {username}
      </Typography>
      {supplied && (
        <Tooltip title="You didn't name a test user for this role, so the platform will use this name. Rename it if you'd rather choose.">
          <Chip size="small" variant="outlined" label="Platform-supplied" />
        </Tooltip>
      )}
      {liveUser && !liveUser.exists && (
        <Chip
          size="small"
          variant="outlined"
          color="info"
          label="Created at Build"
        />
      )}
      {liveUser && liveUser.exists && !liveUser.owned && (
        <Tooltip title="An account with this name already exists and the platform did not create it. It will be left untouched — no password is set, and it is not added to the role. Choose another name.">
          <Chip size="small" color="warning" label="Name already taken" />
        </Tooltip>
      )}
      {liveUser?.owned && actions && (
        <AccountControls
          username={username}
          liveUser={liveUser}
          actions={actions}
        />
      )}
      {onRolesChange && (
        <>
          <Tooltip title="Rename">
            <IconButton
              size="small"
              aria-label={`Rename ${username}`}
              onClick={() => setRenaming(true)}
            >
              <UserPen size={14} />
            </IconButton>
          </Tooltip>
          {!supplied && (
            <Tooltip title="Remove from the design. The account itself is not deleted.">
              <IconButton
                size="small"
                aria-label={`Remove ${username}`}
                onClick={() => onRolesChange(removeTestUser(doc, username))}
              >
                <Trash2 size={14} />
              </IconButton>
            </Tooltip>
          )}
        </>
      )}
    </Stack>
  );
}

/**
 * Reveal / rotate / delete for an account the platform owns.
 *
 * The revealed password lives in this component's state and nowhere else — not
 * in the query cache, not in a URL. It clears when the row unmounts, and the
 * control says "Hide" while it is on screen so leaving it visible is a choice
 * rather than an oversight.
 */
function AccountControls({
  username,
  liveUser,
  actions,
}: {
  username: string;
  liveUser: ProjectTestUserState;
  actions: AccountActions;
}) {
  const [password, setPassword] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = async (fn: () => Promise<string | void>) => {
    setBusy(true);
    setError(null);
    try {
      const result = await fn();
      if (typeof result === "string") setPassword(result);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // Shared accounts outlive the project that named them, so a delete is warned
  // about with the real number rather than a generic "are you sure".
  const alsoUsed = (liveUser.referencingCount ?? 1) - 1;
  const deleteTitle =
    alsoUsed > 0
      ? `Delete this account. ${alsoUsed} other ${alsoUsed === 1 ? "project uses" : "projects use"} it, and would lose its login.`
      : "Delete this account from the identity provider. The role is left standing.";

  return (
    <>
      {password === null ? (
        <Tooltip title="Show this account's password">
          <span>
            <IconButton
              size="small"
              disabled={busy}
              aria-label={`Reveal the password for ${username}`}
              onClick={() => void run(() => actions.reveal(username))}
            >
              <Eye size={14} />
            </IconButton>
          </span>
        </Tooltip>
      ) : (
        <>
          <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
            {password}
          </Typography>
          <Tooltip title="Copy">
            <IconButton
              size="small"
              aria-label={`Copy the password for ${username}`}
              onClick={() => void navigator.clipboard?.writeText(password)}
            >
              <Copy size={14} />
            </IconButton>
          </Tooltip>
          <Button size="small" onClick={() => setPassword(null)}>
            Hide
          </Button>
        </>
      )}
      {/* Rotation invalidates the copy the last build published, so the next
          validation run reads a login that no longer works until Build runs
          again. Saying that here is cheaper than debugging a failed sign-in. */}
      <Tooltip
        title="Set a new password. Anyone holding the current one loses access, including the roles gate ticket the last build published it on — run Build again before validating."
      >
        <span>
          <IconButton
            size="small"
            disabled={busy}
            aria-label={`Rotate the password for ${username}`}
            onClick={() => void run(() => actions.rotate(username))}
          >
            <RotateCcwKey size={14} />
          </IconButton>
        </span>
      </Tooltip>
      <Tooltip title={deleteTitle}>
        <span>
          <IconButton
            size="small"
            disabled={busy}
            aria-label={`Delete the account ${username}`}
            onClick={() => void run(() => actions.remove(username))}
          >
            <Trash2 size={14} />
          </IconButton>
        </span>
      </Tooltip>
      {liveUser.rotatedAt !== null && liveUser.rotatedAt !== undefined && (
        <Typography variant="caption" color="text.secondary">
          rotated {new Date(liveUser.rotatedAt).toLocaleDateString()}
        </Typography>
      )}
      {error !== null && (
        <Typography variant="caption" color="error">
          {error}
        </Typography>
      )}
    </>
  );
}

/** Inline username editor. Validation lives in `rolesDesign.ts` so the panel and
 *  the write gate cannot disagree about what a username may be. */
/** Every username the document already carries, authored or supplied. */
function takenUsernames(doc: RolesDesign): string[] {
  return planUsers(doc).map((u) => u.username);
}

function UsernameField({
  initial,
  taken,
  onCommit,
  onCancel,
}: {
  initial: string;
  /** Names already in the document — a duplicate is rejected here rather than
   *  at the write gate three steps later, where the message is the platform's
   *  rather than the field's. */
  taken: string[];
  onCommit: (name: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(initial);
  const malformed = !/^[a-z0-9][a-z0-9._-]*$/.test(value);
  const duplicate = !malformed && taken.includes(value) && value !== initial;
  const invalid = malformed || duplicate;
  return (
    <Stack direction="row" spacing={1} alignItems="flex-start">
      <TextField
        size="small"
        autoFocus
        value={value}
        onChange={(e) => setValue(e.target.value)}
        error={invalid && value.length > 0}
        helperText={
          duplicate
            ? "Another test user already has this name."
            : malformed && value.length > 0
              ? "Lowercase letters, digits, dot, underscore or hyphen; start with a letter or digit."
              : // The field already reserves this line, so the caution costs
                // nothing and lands where the name is actually chosen.
                "An agent signs in as this account, and its password is published on the build's ticket. Never a real person's name."
        }
        slotProps={{ htmlInput: { "aria-label": "Test user name" } }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !invalid) onCommit(value);
          if (e.key === "Escape") onCancel();
        }}
      />
      <IconButton
        size="small"
        aria-label="Save"
        disabled={invalid}
        onClick={() => onCommit(value)}
      >
        <Check size={16} />
      </IconButton>
      <IconButton size="small" aria-label="Cancel" onClick={onCancel}>
        <X size={16} />
      </IconButton>
    </Stack>
  );
}
