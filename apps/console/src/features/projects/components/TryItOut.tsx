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

import { useMemo, useState } from "react";
import {
  Avatar,
  Box,
  Button,
  IconButton,
  Link as MuiLink,
  ListingTable,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Copy, ExternalLink, FlaskConical } from "@wso2/oxygen-ui-icons-react";
import { StatusChip } from "../../../components/StatusChip";
import { env } from "../../../config/env";
import { thunderUsersConsoleHref } from "../../../config/thunderConsole";
import {
  resourceServerOf,
  useProjectRoles,
  useRevealTestUserPassword,
  type ProjectSignIn,
} from "../../spec/api/roles";
import { cardChip } from "../lib/deploymentLedger";
import type { DeploymentCard } from "../lib/deploymentRows";
import { publishedTestUsers, type PublishedTestUser } from "../lib/publishedTestUsers";
import { tryItAppUrl } from "../lib/tryItAppUrl";
import { AccentPill } from "./AccentPill";
import { PageSection } from "./PageSection";
import { TestUserRow } from "./TestUsersDialog";

// TRY IT OUT (ADR-0032, the Deployment Detail design): each live component
// as a panel a person can act on. A web application is visited, and carries
// the test users it exists to sign in with; a service names its URL and opens
// its contract — its individual endpoints are not listed inline, they live in
// the contract viewer. What the design drew and this does not
// build: a health probe (nothing probes), a token picker, "Get token" and
// "Open app as" (nothing mints a token), and "Run" (the console holds no
// token to run with).

function copyText(value: string): Promise<void> {
  if (!navigator.clipboard?.writeText) {
    return Promise.reject(new Error("Clipboard is not available"));
  }
  return navigator.clipboard.writeText(value);
}

/** "web app" · "service" — the type as a person says it. */
function kindLabel(type: string | undefined): string {
  return type === "web-application" ? "web app" : (type ?? "");
}

/** The URL row: the label, the link, and a copy control. */
function UrlRow({ url, name }: { url: string; name: string }) {
  const [note, setNote] = useState<string | null>(null);
  return (
    <Stack
      direction="row"
      spacing={1.25}
      sx={{ alignItems: "center", px: 2, py: 1, borderTop: 1, borderColor: "divider" }}
    >
      <Typography variant="caption" color="text.secondary" sx={{ width: 40, fontWeight: 700, letterSpacing: "0.06em" }}>
        URL
      </Typography>
      <MuiLink
        href={url}
        target="_blank"
        rel="noreferrer"
        variant="body2"
        sx={{
          fontFamily: "monospace",
          display: "inline-flex",
          alignItems: "center",
          gap: 0.5,
          minWidth: 0,
          flexGrow: 1,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        {url} <ExternalLink size={13} aria-hidden />
      </MuiLink>
      <Tooltip title={note ?? "Copy URL"}>
        <IconButton
          size="small"
          aria-label={`Copy the URL of ${name}`}
          onClick={() =>
            void copyText(url)
              .then(() => setNote("Copied"))
              .catch((e: unknown) => setNote(e instanceof Error ? e.message : String(e)))
          }
        >
          <Copy size={14} />
        </IconButton>
      </Tooltip>
    </Stack>
  );
}

// ── Test users ──────────────────────────────────────────────────────────────

const USERS_SHOWN = 5;

/**
 * The environment's test accounts, inline: they exist to sign in to the app
 * above them, so they sit inside its panel. Filterable, and folded past five
 * rows so a many-role app does not push the API panel off the screen.
 */
export function TestUsersInline({
  logins,
  loadState,
  thunderUrl,
  revealPassword,
}: {
  logins: readonly PublishedTestUser[];
  loadState: "ready" | "pending" | "error";
  thunderUrl: string;
  revealPassword: (username: string) => Promise<string>;
}) {
  const [query, setQuery] = useState("");
  const [all, setAll] = useState(false);
  const q = query.trim().toLowerCase();
  const matching = logins.filter(
    (l) =>
      q === "" ||
      l.username.toLowerCase().includes(q) ||
      l.roles.some((r) => r.toLowerCase().includes(q)),
  );
  const shown = all ? matching : matching.slice(0, USERS_SHOWN);
  return (
    <Box sx={{ borderTop: 1, borderColor: "divider" }}>
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 1, px: 2, py: 1.25 }}
      >
        <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
          Sign in with a test user
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {loadState === "ready"
            ? `${logins.length} account${logins.length === 1 ? "" : "s"} · one per role · Development only`
            : loadState === "pending"
              ? "Loading test users…"
              : "Couldn't load test users."}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        {logins.length > USERS_SHOWN && (
          <TextField
            size="small"
            placeholder="Filter by role or name"
            inputProps={{ "aria-label": "Filter test users by role or name" }}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            sx={{ width: 220 }}
          />
        )}
        <MuiLink
          href={thunderUsersConsoleHref(thunderUrl)}
          target="_blank"
          rel="noreferrer"
          variant="body2"
          aria-label="Open Thunder Console to add or remove real accounts"
          sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}
        >
          Manage in Thunder Console <ExternalLink size={12} aria-hidden />
        </MuiLink>
      </Stack>
      {loadState === "ready" && logins.length > 0 && (
        <>
          <ListingTable.Container sx={{ width: "100%" }}>
            <ListingTable density="compact">
              <ListingTable.Head>
                <ListingTable.Row>
                  <ListingTable.Cell>Account</ListingTable.Cell>
                  <ListingTable.Cell sx={{ width: 240 }}>Password</ListingTable.Cell>
                  <ListingTable.Cell sx={{ width: 160 }}>Role</ListingTable.Cell>
                  {/* "Cold start" was a v1 leftover — the cell has always
                      held `login.scopes`, never a cold-start anything. */}
                  <ListingTable.Cell sx={{ width: 140 }}>Scopes</ListingTable.Cell>
                </ListingTable.Row>
              </ListingTable.Head>
              <ListingTable.Body>
                {shown.map((login) => (
                  <TestUserRow key={login.username} login={login} revealPassword={revealPassword} />
                ))}
              </ListingTable.Body>
            </ListingTable>
          </ListingTable.Container>
          {matching.length > USERS_SHOWN && (
            <Stack
              direction="row"
              sx={{ alignItems: "center", justifyContent: "space-between", px: 2, py: 1, borderTop: 1, borderColor: "divider" }}
            >
              <Typography variant="caption" color="text.secondary">
                Showing {shown.length} of {matching.length}
              </Typography>
              <Button size="small" onClick={() => setAll((v) => !v)}>
                {all ? "Show fewer" : "Show all"}
              </Button>
            </Stack>
          )}
          {matching.length === 0 && (
            <Typography variant="body2" color="text.secondary" sx={{ px: 2, py: 1.5 }}>
              No account matches.
            </Typography>
          )}
        </>
      )}
      {loadState === "ready" && logins.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ px: 2, pb: 1.5 }}>
          No test users were published for this version.
        </Typography>
      )}
    </Box>
  );
}

/** Live wiring for the inline panel. Mounted only for a green development. */
export function useTestUsers(projectName: string, enabled: boolean) {
  const live = useProjectRoles(projectName, enabled);
  const reveal = useRevealTestUserPassword(projectName);
  const loadState = live.isPending ? "pending" : live.isError ? "error" : "ready";
  const logins =
    loadState === "ready" ? publishedTestUsers(live.data?.testUsers ?? []) : [];
  // Where a client outside the project signs in, and as which client: what the
  // test app needs to sign a person in as one of these accounts. Absent until
  // the roles read answers, so the agent panel offers no launch it could not
  // honour.
  const signIn = live.data?.signIn;
  const resource = resourceServerOf(live.data);
  return {
    projectName,
    logins,
    loadState: loadState as "ready" | "pending" | "error",
    thunderUrl: env.thunderUrl,
    ...(signIn && resource ? { signIn: { ...signIn, resource } } : {}),
    revealPassword: async (username: string) => {
      const data = await reveal.mutateAsync(username);
      return data.password;
    },
  };
}

// ── The panels ──────────────────────────────────────────────────────────────

export interface TestUsersProps {
  projectName: string;
  logins: readonly PublishedTestUser[];
  /**
   * The project's sign-in as a client outside it would perform it. Present
   * only when the project has a sign-in client and a resource server — the two
   * facts the test app cannot learn on its own.
   */
  signIn?: ProjectSignIn & { resource: string };
  loadState: "ready" | "pending" | "error";
  thunderUrl: string;
  revealPassword: (username: string) => Promise<string>;
}

/**
 * One component's panel: identity, its release, its state, its way in, its
 * URL — and what a person does with it next: the accounts for a web app,
 * the contract viewer for a service.
 */
function ComponentPanel({
  card,
  type,
  talksTo,
  testUsers,
  onTryApi,
}: {
  card: DeploymentCard;
  type: string | undefined;
  talksTo: string[];
  testUsers: TestUsersProps | null;
  onTryApi: () => void;
}) {
  const chip = cardChip(card);
  const d = card.deployment;
  const isWebApp = type === "web-application";
  const isService = type === "service";
  const isAgent = type === "ai-agent";
  const serving = card.kind === "success";
  const kind = kindLabel(type);
  return (
    <Box
      sx={{
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        overflow: "hidden",
        ...(card.kind === "notDeployed" && { opacity: 0.6, borderStyle: "dashed" }),
      }}
    >
      <Stack direction="row" spacing={1.5} sx={{ alignItems: "center", px: 2, py: 1.5, flexWrap: "wrap", rowGap: 1 }}>
        <Avatar sx={{ width: 28, height: 28, bgcolor: "action.hover", color: "text.primary", fontSize: 13 }}>
          {(card.displayName.trim()[0] ?? "C").toUpperCase()}
        </Avatar>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, flexShrink: 0 }}>
          {card.displayName}
        </Typography>
        {d?.releaseName && (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ fontFamily: "monospace", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0 }}
          >
            {d.releaseName}
          </Typography>
        )}
        {kind && (
          <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: "nowrap" }}>
            · {kind}
          </Typography>
        )}
        <Box sx={{ flexGrow: 1 }} />
        <StatusChip label={chip.label} tone={chip.tone} {...(chip.outlined && { variant: "outlined" as const })} />
        {isWebApp && d?.endpointUrl && (
          <Button
            variant="contained"
            size="small"
            href={d.endpointUrl}
            target="_blank"
            rel="noreferrer"
            aria-label={`Visit ${card.displayName}`}
            endIcon={<ExternalLink size={13} aria-hidden />}
          >
            Visit app
          </Button>
        )}
        {/* An agent is a secured service with no page of its own: the platform's
            test app is the page, signing a person in as one of the project's
            test users. Offered only while there is an account to sign in as and
            a client to sign in through — without either the app could only
            explain why it cannot. */}
        {isAgent && serving && d?.endpointUrl && testUsers?.signIn && testUsers.logins.length > 0 && (
          <Button
            variant="contained"
            size="small"
            href={tryItAppUrl(env.tryItUrl, {
              project: testUsers.projectName,
              component: card.componentName,
              issuer: testUsers.signIn.issuer,
              clientId: testUsers.signIn.clientId,
              resource: testUsers.signIn.resource,
              // profile + email so the test app can show WHO is signed in — the
              // ID token's `sub` is a UUID. Both are public claims of a test user.
              scopes: ["openid", "profile", "email", ...new Set(testUsers.logins.flatMap((login) => login.scopes))],
              endpoint: d.endpointUrl,
            })}
            target="_blank"
            rel="noreferrer"
            aria-label={`Try ${card.displayName}`}
            endIcon={<ExternalLink size={13} aria-hidden />}
          >
            Try agent
          </Button>
        )}
        {/* Only a SERVING service is worth trying — an undeployed or failed
            one has a contract but nothing behind it. */}
        {isService && serving && (
          <AccentPill
            onClick={onTryApi}
            aria-label={`Try ${card.displayName} API`}
            startIcon={<FlaskConical size={13} aria-hidden />}
          >
            Try API
          </AccentPill>
        )}
      </Stack>
      {d?.endpointUrl && <UrlRow url={d.endpointUrl} name={card.displayName} />}
      {isWebApp && talksTo.length > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block", px: 2, pb: 1.25 }}>
          Talks to{" "}
          <Box component="span" sx={{ fontWeight: 600 }}>
            {talksTo.join(", ")}
          </Box>{" "}
          on this environment
        </Typography>
      )}
      {isWebApp && testUsers && <TestUsersInline {...testUsers} />}
    </Box>
  );
}

/**
 * The "Try it out" card: every component of the environment as a panel. A web
 * application leads — it is the thing a person opens, and the test users sit
 * inside the first one's panel because they exist to sign in to it (under
 * their own heading when the project has no web app at all). The services
 * follow in the board's order.
 */
export function TryItOutCard({
  cards,
  types,
  talksTo,
  testUsers,
  onTryApi,
}: {
  cards: DeploymentCard[];
  types: Map<string, string>;
  talksTo: (componentName: string) => string[];
  /** The accounts, when this environment has them (a green development). */
  testUsers: TestUsersProps | null;
  onTryApi: (componentName: string) => void;
}) {
  // Stable: the web apps keep their order among themselves, and so do the rest.
  const ordered = useMemo(
    () =>
      [...cards].sort(
        (a, b) =>
          Number(types.get(b.componentName) === "web-application") -
          Number(types.get(a.componentName) === "web-application"),
      ),
    [cards, types],
  );
  const firstWebApp = ordered.find((c) => types.get(c.componentName) === "web-application");
  // An agent's panel gets the accounts too — for the launch, not for a list.
  const withAccounts = (card: DeploymentCard) =>
    card === firstWebApp || types.get(card.componentName) === "ai-agent";
  return (
    // Section 2 of the environment page. Web apps lead, the services they
    // call follow — the thing a person opens first, first.
    <PageSection title="Try it out">
      <Stack spacing={3}>
        {ordered.map((card) => (
          <ComponentPanel
            key={card.componentName}
            card={card}
            type={types.get(card.componentName)}
            talksTo={talksTo(card.componentName)}
            testUsers={testUsers && withAccounts(card) ? testUsers : null}
            onTryApi={() => onTryApi(card.componentName)}
          />
        ))}
        {testUsers && !firstWebApp && (
          <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, overflow: "hidden" }}>
            <TestUsersInline {...testUsers} />
          </Box>
        )}
      </Stack>
    </PageSection>
  );
}
