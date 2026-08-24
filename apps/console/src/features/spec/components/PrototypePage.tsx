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

import { useMemo } from "react";
import { Alert, Box, CircularProgress, PageContent, Typography } from "@wso2/oxygen-ui";
import { Link } from "@tanstack/react-router";
import { PrototypeView } from "@aep/ui-excalidraw-view";
import { PageHeader } from "../../../components/PageHeader";
import { useSpecFiles } from "../api/queries";
import { buildDesignSection } from "../api/designTree";
import { useDerivedPrototype } from "../api/useDerivedDesign";

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <Box
      sx={{
        flex: 1,
        minHeight: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      {children}
    </Box>
  );
}

/**
 * Full-screen, deep-linkable prototype for one design component's wireframes
 * (#348). Mirrors WireframePanel's inline "Prototype" mode, but as the
 * whole page: resolve the component's `.dsl` via `buildDesignSection` (the
 * same tree WireframePanel/SpecView use for the sidebar), derive the model,
 * and hand off to `PrototypeView` with the deep-linked `screen` and `flow` —
 * the route wrapper syncs `onScreenChange`/`onFlowChange` back into
 * `?screen=`/`?flow=` via replace-navigation, so a shared link restores both
 * the screen and the persona.
 */
export function PrototypePage({
  projectName,
  component,
  screen,
  flow,
  onScreenChange,
  onFlowChange,
}: {
  projectName: string;
  component: string;
  screen?: string;
  flow?: string;
  onScreenChange: (screen: string) => void;
  onFlowChange: (flow: string) => void;
}) {
  const filesQuery = useSpecFiles(projectName);
  const node = useMemo(() => {
    if (!filesQuery.data) return undefined;
    return buildDesignSection(filesQuery.data).components.find(
      (c) => c.name === component,
    );
  }, [filesQuery.data, component]);
  const dslPath = node?.wireframeDslPath ?? "";
  const sha = filesQuery.data?.find((f) => f.path === dslPath)?.sha;
  const { model, isPending: modelPending } = useDerivedPrototype(
    projectName,
    dslPath,
    sha,
  );

  let body: React.ReactNode;
  if (filesQuery.isPending) {
    body = (
      <Centered>
        <CircularProgress aria-label="Loading spec files" />
      </Centered>
    );
  } else if (filesQuery.isError) {
    body = <Alert severity="error">Failed to load the spec files.</Alert>;
  } else if (!node || !node.wireframeDslPath) {
    body = (
      <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>
        This component has no wireframes yet.
      </Typography>
    );
  } else if (modelPending) {
    body = (
      <Centered>
        <CircularProgress aria-label="Loading prototype" />
      </Centered>
    );
  } else if (!model) {
    body = (
      <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>
        This wireframe could not be rendered as a prototype.
      </Typography>
    );
  } else {
    body = (
      <PrototypeView
        key={sha ?? dslPath}
        model={model}
        fillHeight
        onScreenChange={onScreenChange}
        onFlowChange={onFlowChange}
        {...(screen ? { initialScreen: screen } : {})}
        {...(flow ? { initialFlow: flow } : {})}
      />
    );
  }

  return (
    <PageContent
      fullWidth
      noPadding
      sx={{ height: "100%", display: "flex", flexDirection: "column" }}
    >
      <Box
        sx={{
          height: "100%",
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        <PageHeader
          title={component}
          backTo={{
            link: (
              <Link
                to="/projects/$projectName/spec"
                params={{ projectName }}
              />
            ),
            label: "Back to Spec",
          }}
        />
        <Box
          sx={{
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          {body}
        </Box>
      </Box>
    </PageContent>
  );
}
