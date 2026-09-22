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
 * The prototype review page for one web-application component: reads its
 * `prototype.json` and, when the file is a valid model, hands it to the
 * review shell. Every other outcome — loading, a failed read, no prototype,
 * a file that fails the model — renders a message and nothing of the
 * application: a prototype never renders partially.
 *
 * The page takes over the whole viewport (the console's chrome steps aside
 * for this route, see AppLayout); these fallbacks keep the one way out.
 */

import type { ReactElement, ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  CircularProgress,
  PageTitle,
  Paper,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { usePrototype, type PrototypeReadIssue } from "../api/queries";
import type { PrototypeViewRequest } from "../model/viewState";
import { PrototypeShell } from "./PrototypeShell";

export interface ComponentPrototypePageProps {
  projectName: string;
  component: string;
  /** The view the URL asks for. */
  search: PrototypeViewRequest;
  /** Called with the whole view the URL should carry whenever it changes. */
  onSearchChange: (next: PrototypeViewRequest) => void;
}

export function ComponentPrototypePage({ projectName, component, search, onSearchChange }: ComponentPrototypePageProps) {
  const prototype = usePrototype(projectName, component);
  const backLink = <Link to="/projects/$projectName/spec" params={{ projectName }} />;

  if (prototype.isPending) {
    return (
      <Fallback backLink={backLink} component={component}>
        <CircularProgress aria-label="Loading prototype" />
      </Fallback>
    );
  }
  if (prototype.isError) {
    return (
      <Fallback backLink={backLink} component={component}>
        <Alert
          severity="error"
          action={
            <Button color="inherit" size="small" onClick={() => void prototype.refetch()}>
              Retry
            </Button>
          }
        >
          <AlertTitle>Couldn't load the prototype</AlertTitle>
          {prototype.error.message}
        </Alert>
      </Fallback>
    );
  }
  const read = prototype.data;
  if (read === null) {
    return (
      <Fallback backLink={backLink} component={component}>
        <Typography color="text.secondary">{component} has no prototype yet.</Typography>
      </Fallback>
    );
  }
  if (!read.ok) {
    return (
      <Fallback backLink={backLink} component={component}>
        <InvalidPrototype issues={read.issues} />
      </Fallback>
    );
  }
  return (
    <PrototypeShell
      // A different file is a different review: start its view state fresh
      // from the URL rather than repairing the old one.
      key={component}
      model={read.model}
      request={search}
      onRequestChange={onSearchChange}
      backLink={backLink}
    />
  );
}

function InvalidPrototype({ issues }: { issues: PrototypeReadIssue[] }) {
  return (
    <Alert severity="error" sx={{ maxWidth: 720, width: "100%" }}>
      <AlertTitle>This prototype can't be shown</AlertTitle>
      <Typography variant="body2" sx={{ mb: 1 }}>
        The file does not match the prototype model, so nothing of it is rendered.
      </Typography>
      <Stack component="ul" spacing={0.5} sx={{ m: 0, pl: 2 }}>
        {issues.map((issue, i) => (
          <Typography key={i} component="li" variant="body2">
            <Box component="code" sx={{ fontFamily: "monospace", fontWeight: "fontWeightMedium" }}>
              {issue.code}
            </Box>
            {issue.path && (
              <Box component="span" sx={{ fontFamily: "monospace" }}>
                {` at ${issue.path}`}
              </Box>
            )}
            {` — ${issue.message}`}
          </Typography>
        ))}
      </Stack>
    </Alert>
  );
}

function Fallback({
  backLink,
  component,
  children,
}: {
  backLink: ReactElement<{ children?: ReactNode }>;
  component: string;
  children: ReactNode;
}) {
  return (
    <Box sx={{ height: "100vh", display: "flex", flexDirection: "column", bgcolor: "background.default" }}>
      <Paper square elevation={0} component="header" sx={{ px: 2, py: 1, borderBottom: 1, borderColor: "divider" }}>
        <Stack direction="row" alignItems="center" spacing={2}>
          <Box sx={{ width: "fit-content" }}>
            <PageTitle.BackButton component={backLink}>Back to Spec</PageTitle.BackButton>
          </Box>
          <Typography variant="subtitle1" component="h1">
            {component}
          </Typography>
        </Stack>
      </Paper>
      <Box sx={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center", p: 3 }}>{children}</Box>
    </Box>
  );
}
