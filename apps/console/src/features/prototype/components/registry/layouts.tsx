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

// Layouts: stack, grid, split, detail — and the app shell a screen renders in
// when it names a navigation.

import type { ReactNode } from "react";
import { Box, Card, CardContent, CardHeader, Grid, Stack, Typography } from "@wso2/oxygen-ui";
import type {
  PrototypeDetailNode,
  PrototypeGridNode,
  PrototypeNavigation,
  PrototypeSplitNode,
  PrototypeStackNode,
} from "@aep/prototype-model";
import { isShownIn } from "../../model/visibility";
import { usePrototypeRender } from "../renderContext";
import { SideNavigation, TopNavigation } from "./navigation";

export function StackView({ node }: { node: PrototypeStackNode }) {
  const { renderNodes } = usePrototypeRender();
  const row = node.direction === "row";
  return (
    <Stack direction={row ? "row" : "column"} spacing={2} {...(row ? { alignItems: "center", flexWrap: "wrap", useFlexGap: true } : {})}>
      {renderNodes(node.content)}
    </Stack>
  );
}

export function GridView({ node }: { node: PrototypeGridNode }) {
  const { renderNodes, view } = usePrototypeRender();
  const span = Math.max(1, Math.floor(12 / Math.max(1, node.columns)));
  // One cell per child the display state shows, so hidden children leave no gap.
  return (
    <Grid container spacing={2}>
      {node.content
        .filter((child) => isShownIn(child, view.stateId))
        .map((child) => (
          <Grid key={child.id} size={{ xs: 12, md: span }}>
            {renderNodes([child])}
          </Grid>
        ))}
    </Grid>
  );
}

export function SplitView({ node }: { node: PrototypeSplitNode }) {
  const { renderNodes } = usePrototypeRender();
  const left = Math.min(11, Math.max(1, node.ratio ?? 6));
  return (
    <Grid container spacing={3}>
      <Grid size={{ xs: 12, md: left }}>
        <Stack spacing={3}>{renderNodes(node.left)}</Stack>
      </Grid>
      <Grid size={{ xs: 12, md: 12 - left }}>
        <Stack spacing={3}>{renderNodes(node.right)}</Stack>
      </Grid>
    </Grid>
  );
}

export function DetailView({ node }: { node: PrototypeDetailNode }) {
  return (
    <Card variant="outlined">
      {node.title && <CardHeader title={node.title} />}
      <CardContent>
        <Grid container spacing={2} component="dl" sx={{ m: 0 }}>
          {node.fields.map((f) => (
            <Grid key={f.label} size={{ xs: 12, sm: 6 }}>
              <Typography variant="overline" color="text.secondary" component="dt">
                {f.label}
              </Typography>
              <Typography variant="body1" component="dd" sx={{ m: 0 }}>
                {f.value}
              </Typography>
            </Grid>
          ))}
        </Grid>
      </CardContent>
    </Card>
  );
}

/**
 * The application's own chrome around a screen (the registry's `app-shell`):
 * a side navigation beside the content, or a top navigation above it. A
 * screen without a navigation renders bare.
 */
export function AppShellView({ navigation, children }: { navigation: PrototypeNavigation | undefined; children: ReactNode }) {
  const body = (
    <Box component="main" sx={{ flex: 1, minWidth: 0, minHeight: 0, overflow: "auto", p: 4, bgcolor: "background.default" }}>
      <Box sx={{ maxWidth: 1100, mx: "auto" }}>{children}</Box>
    </Box>
  );
  if (!navigation) return <Box sx={{ display: "flex", flex: 1, minHeight: 0 }}>{body}</Box>;
  if (navigation.kind === "top-nav") {
    return (
      <Box sx={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }}>
        <TopNavigation navigation={navigation} />
        {body}
      </Box>
    );
  }
  return (
    <Box sx={{ display: "flex", flex: 1, minHeight: 0 }}>
      <SideNavigation navigation={navigation} />
      {body}
    </Box>
  );
}
