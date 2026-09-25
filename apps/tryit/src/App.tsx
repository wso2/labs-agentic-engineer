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

import { useEffect, useMemo, useState } from "react";
import { Alert, AlertTitle, Box, Button, CircularProgress, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { KeyRound } from "@wso2/oxygen-ui-icons-react";
import { AgentScreen } from "./AgentScreen";
import { env } from "./env";
import { endpointAllowed, loadLaunch, parseLaunch, saveLaunch, type Launch } from "./launch";
import { createSession, type Session } from "./session";

const CALLBACK_PATH = "/callback";

/**
 * The launch is what the console put in the hash, or — after the identity
 * provider redirected back to /callback with no hash — what this tab saved.
 * Read once: the URL is rewritten to `/` after the callback is finished.
 */
function currentLaunch(): Launch | null {
  const fromUrl = parseLaunch(window.location.hash);
  if (fromUrl) {
    saveLaunch(fromUrl);
    return fromUrl;
  }
  return loadLaunch();
}

type Phase =
  | { kind: "checking" }
  | { kind: "signed-out" }
  | { kind: "signed-in"; token: string; username: string | null }
  | { kind: "callback-failed"; message: string };

export function App() {
  const [launch] = useState(currentLaunch);
  // A launch anyone could have written pairs a real sign-in with any endpoint;
  // the operator's allowlist decides where a token may go, before one exists.
  const trusted = launch !== null && endpointAllowed(launch.endpoint, env.gatewayHosts);
  const session = useMemo(() => (launch && trusted ? createSession(launch) : null), [launch, trusted]);
  const [phase, setPhase] = useState<Phase>({ kind: "checking" });

  useEffect(() => {
    if (!session) return;
    let cancelled = false;
    const settle = async () => {
      if (window.location.pathname === CALLBACK_PATH) {
        try {
          await session.handleCallback();
        } catch (error) {
          if (!cancelled) {
            setPhase({ kind: "callback-failed", message: error instanceof Error ? error.message : String(error) });
          }
          return;
        } finally {
          // The code in the URL is spent; a reload must not replay it.
          window.history.replaceState(null, "", "/");
        }
      }
      const token = await session.accessToken();
      if (cancelled) return;
      if (!token) {
        setPhase({ kind: "signed-out" });
        return;
      }
      setPhase({ kind: "signed-in", token, username: await session.username() });
    };
    void settle();
    return () => {
      cancelled = true;
    };
  }, [session]);

  if (launch && !trusted) {
    return (
      <Shell>
        <Alert severity="error">
          <AlertTitle>This link points at an address the test app will not send a token to</AlertTitle>
          <Typography variant="body2" sx={{ fontFamily: "monospace", wordBreak: "break-all", mb: 1 }}>
            {launch.endpoint}
          </Typography>
          Only the platform's gateway hosts are allowed ({env.gatewayHosts.join(", ")}). Open the agent
          again from the console; if its address is right, the operator extends the allowlist.
        </Alert>
      </Shell>
    );
  }

  if (!launch || !session) {
    return (
      <Shell>
        <Alert severity="info">
          <AlertTitle>Open this app from the console</AlertTitle>
          In the console, open the project's <strong>Deployments</strong> page and, under Try it out,
          choose <strong>Try agent</strong> on an agent. The console hands this app the project's sign-in and the agent's address; there is
          nothing to configure here.
        </Alert>
      </Shell>
    );
  }

  if (phase.kind === "signed-in") {
    return (
      <AgentScreen
        launch={launch}
        token={phase.token}
        username={phase.username}
        onSignOut={async () => {
          await session.signOut();
          setPhase({ kind: "signed-out" });
        }}
      />
    );
  }

  return (
    <Shell>
      <SignInCard launch={launch} session={session} phase={phase} />
    </Shell>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center", px: 2, py: 4, bgcolor: "background.default" }}>
      <Box sx={{ width: "100%", maxWidth: 520 }}>{children}</Box>
    </Box>
  );
}

function SignInCard({ launch, session, phase }: { launch: Launch; session: Session; phase: Phase }) {
  return (
    <Paper variant="outlined" sx={{ p: 3 }}>
      <Stack spacing={2}>
        <Stack spacing={0.5}>
          <Typography variant="overline" color="text.secondary">
            {launch.project}
          </Typography>
          <Typography variant="h5" component="h1" sx={{ fontWeight: 600 }}>
            {launch.component}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            This agent needs the project's sign-in. Sign in as one of the project's test users. Their
            usernames and passwords are in the console, under <strong>Deployments → Try it out</strong>
            (reveal the password next to the user).
          </Typography>
        </Stack>
        {phase.kind === "callback-failed" && (
          <Alert severity="error">
            <AlertTitle>The sign-in did not complete</AlertTitle>
            {phase.message}
          </Alert>
        )}
        {phase.kind === "checking" ? (
          <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }} aria-live="polite">
            <CircularProgress size={18} />
            <Typography variant="body2" color="text.secondary">
              Checking the session…
            </Typography>
          </Stack>
        ) : (
          <Button
            variant="contained"
            size="large"
            startIcon={<KeyRound size={16} aria-hidden />}
            onClick={() => void session.signIn()}
          >
            Sign in as a test user
          </Button>
        )}
        <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace", wordBreak: "break-all" }}>
          {launch.endpoint}
        </Typography>
      </Stack>
    </Paper>
  );
}
