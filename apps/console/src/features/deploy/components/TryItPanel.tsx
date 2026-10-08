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

import { useState, type ReactNode } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogContent,
  DialogTitle,
  IconButton,
  Link,
  Typography,
} from "@wso2/oxygen-ui";
import { ExternalLink, X } from "@wso2/oxygen-ui-icons-react";
import { OpenApiView } from "@aep/ui-openapi-view";
import { env } from "../../../config/env";
import { useComponentOpenApi, useProjectRoles, useRevealTestUserPassword, type ProjectRolesView } from "../api/deploy";
import { componentKindLabel, type ColumnComponent, type EnvironmentColumn } from "../model/pipeline";
import { Segmented } from "../../design/components/Segmented";
import { agentLaunch, testLogins, tryItAppUrl, tryItKind, type EnvironmentRef, type TestLogin } from "../model/tryIt";

// Try it, a Panel over the Deploy Page and the project overview: no address,
// and the chat stays as it was. Each component serving in the environment, as
// a person tries it: a web app opened and signed in to as one of the
// project's test users, an agent opened in the platform's test app the same
// way, a service's API. From the overview it is one component's, with a
// switch between the environments it serves in.
//
// The console cannot sign anyone in on the project's identity provider: the
// sign-in happens on its page. So "Open as" opens the app and copies that
// test user's password for the sign-in it asks for.

interface Logins {
  state: "pending" | "error" | "ready";
  logins: TestLogin[];
  view: ProjectRolesView | undefined;
  reveal: (username: string) => Promise<string>;
}

function SignInAs({ href, logins }: { href: string; logins: Logins }) {
  if (logins.state === "pending") return <CircularProgress size={16} />;
  if (logins.state === "error") {
    return (
      <Typography variant="caption" color="text.secondary">
        The project&apos;s test users could not be read.
      </Typography>
    );
  }
  if (logins.logins.length === 0) {
    return (
      <Typography variant="caption" color="text.secondary">
        This project has no test users to sign in as.
      </Typography>
    );
  }
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      {logins.logins.map((login) => (
        <LoginRow key={login.username} login={login} href={href} reveal={logins.reveal} />
      ))}
    </Box>
  );
}

function LoginRow({ login, href, reveal }: { login: TestLogin; href: string; reveal: (username: string) => Promise<string> }) {
  const [note, setNote] = useState<{ text: string; password?: string } | null>(null);
  // The link opens the app at once; the password is read and copied meanwhile,
  // ready for the sign-in the app asks for.
  const copyPassword = async () => {
    let password: string;
    try {
      password = await reveal(login.username);
    } catch (e) {
      setNote({ text: e instanceof Error ? e.message : String(e) });
      return;
    }
    try {
      await navigator.clipboard.writeText(password);
      setNote({ text: `${login.username}'s password is copied. Paste it when the app asks you to sign in.` });
    } catch {
      setNote({ text: `Sign in as ${login.username} with this password:`, password });
    }
  };
  return (
    <Box>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Typography variant="body2" sx={{ flex: 1, minWidth: 0 }}>
          <Box component="span" sx={{ fontFamily: "monospace", fontWeight: 600 }}>
            {login.username}
          </Box>{" "}
          <Typography component="span" variant="caption" color="text.secondary">
            {login.roles.join(", ")}
          </Typography>
        </Typography>
        <Button
          size="small"
          variant="outlined"
          href={href}
          target="_blank"
          rel="noreferrer"
          endIcon={<ExternalLink size={14} aria-hidden />}
          onClick={() => void copyPassword()}
        >
          Open as {login.username}
        </Button>
      </Box>
      {note && (
        <Typography variant="caption" color="text.secondary" role="status" sx={{ display: "block" }}>
          {note.text}
          {note.password && (
            <Box component="span" sx={{ fontFamily: "monospace", ml: 0.5, color: "text.primary" }}>
              {note.password}
            </Box>
          )}
        </Typography>
      )}
    </Box>
  );
}

function Section({ component, children }: { component: ColumnComponent; children: ReactNode }) {
  const kind = componentKindLabel(component.type);
  return (
    <Box component="section" aria-label={component.displayName} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Box>
        <Typography sx={{ fontWeight: 600 }}>
          {component.displayName}
          {kind && (
            <Typography component="span" variant="caption" color="text.secondary" sx={{ fontWeight: 400 }}>
              {" "}
              · {kind}
            </Typography>
          )}
        </Typography>
        {component.url && (
          <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace", wordBreak: "break-all" }}>
            {component.url}
          </Typography>
        )}
      </Box>
      {children}
      {component.deployment?.agentManagerUrl && (
        <Link
          href={component.deployment.agentManagerUrl}
          target="_blank"
          rel="noreferrer"
          variant="body2"
          sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, alignSelf: "flex-start" }}
        >
          Manage in Agent Manager <ExternalLink size={12} aria-hidden />
        </Link>
      )}
    </Box>
  );
}

function ServiceApi({ projectName, component }: { projectName: string; component: ColumnComponent }) {
  const [open, setOpen] = useState(false);
  const spec = useComponentOpenApi(projectName, component.name, open);
  if (!open) {
    return (
      <Button size="small" variant="outlined" sx={{ alignSelf: "flex-start" }} onClick={() => setOpen(true)}>
        Show its API
      </Button>
    );
  }
  if (spec.isPending) return <CircularProgress size={16} />;
  if (spec.isError) return <Alert severity="error">{spec.error.message}</Alert>;
  return (
    <Box sx={{ height: "60vh", border: 1, borderColor: "divider", borderRadius: 2, overflow: "hidden" }}>
      <OpenApiView spec={spec.data} />
    </Box>
  );
}

/** One component, opened from the overview, and the environments it can be tried in. */
export interface TryItFocus {
  componentName: string;
  environments: EnvironmentRef[];
  onEnvironment: (name: string) => void;
}

/** Try it: what each component serving in one environment offers a person, or one component's. */
export function TryItPanel({
  projectName,
  column,
  entryLabel,
  focus,
  onClose,
}: {
  projectName: string;
  column: EnvironmentColumn;
  /** The pipeline's first environment, the one the project's test users are set up in. */
  entryLabel: string;
  focus?: TryItFocus;
  onClose: () => void;
}) {
  const serving = column.components.flatMap((c) => {
    const kind = tryItKind(c);
    return kind && (!focus || c.name === focus.componentName) ? [{ component: c, kind }] : [];
  });
  const focused = focus ? column.components.find((c) => c.name === focus.componentName) : undefined;
  const signsIn = column.entry && serving.some((s) => s.kind !== "service");
  const roles = useProjectRoles(projectName, signsIn);
  const reveal = useRevealTestUserPassword(projectName);
  const logins: Logins = {
    state: roles.isPending ? "pending" : roles.isError ? "error" : "ready",
    logins: testLogins(roles.data),
    view: roles.data,
    reveal: (username) => reveal.mutateAsync(username),
  };
  const elsewhere = (
    <Typography variant="caption" color="text.secondary">
      The project&apos;s test users are set up in {entryLabel} only.
    </Typography>
  );

  return (
    <Dialog open onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle sx={{ pr: 6 }}>
        Try {focused?.displayName ?? column.version ?? "it"} in {column.label}
        <IconButton aria-label="Close" size="small" onClick={onClose} sx={{ position: "absolute", right: 12, top: 12 }}>
          <X size={18} />
        </IconButton>
      </DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
        {focus && focus.environments.length > 1 && (
          <Box sx={{ alignSelf: "flex-start" }}>
            <Segmented
              label="Environment"
              value={column.name}
              options={focus.environments.map((e) => ({ value: e.name, label: e.label }))}
              onChange={focus.onEnvironment}
            />
          </Box>
        )}
        {serving.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            {focus
              ? `${focused?.displayName ?? focus.componentName} is not serving in ${column.label} right now.`
              : `Nothing is serving in ${column.label} yet.`}
          </Typography>
        )}
        {serving.map(({ component, kind }) => (
          <Section key={component.name} component={component}>
            {kind === "web-app" &&
              (column.entry ? <SignInAs href={component.url ?? ""} logins={logins} /> : (
                <>
                  <Button
                    size="small"
                    variant="outlined"
                    href={component.url ?? ""}
                    target="_blank"
                    rel="noreferrer"
                    endIcon={<ExternalLink size={14} aria-hidden />}
                    sx={{ alignSelf: "flex-start" }}
                  >
                    Open the app
                  </Button>
                  {elsewhere}
                </>
              ))}
            {kind === "agent" && <AgentTry projectName={projectName} component={component} entry={column.entry} logins={logins} elsewhere={elsewhere} />}
            {kind === "service" && <ServiceApi projectName={projectName} component={component} />}
          </Section>
        ))}
      </DialogContent>
    </Dialog>
  );
}

/** An agent has no page of its own: the platform's test app is its page, signed in to as a test user. */
function AgentTry({
  projectName,
  component,
  entry,
  logins,
  elsewhere,
}: {
  projectName: string;
  component: ColumnComponent;
  entry: boolean;
  logins: Logins;
  elsewhere: ReactNode;
}) {
  if (!entry) return elsewhere;
  if (logins.state !== "ready") return <SignInAs href="" logins={logins} />;
  const launch = agentLaunch(projectName, component, logins.view);
  if (!launch) {
    return (
      <Typography variant="caption" color="text.secondary">
        This project has no sign-in for the test app to use, or no test users to sign in as.
      </Typography>
    );
  }
  return <SignInAs href={tryItAppUrl(env.tryItUrl, launch)} logins={logins} />;
}
