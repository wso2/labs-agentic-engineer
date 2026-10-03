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

import { createLink, useParams } from "@tanstack/react-router";
import { Box, Tab, Tabs } from "@wso2/oxygen-ui";
import { usePrototypeDot } from "../../prototype/usePrototypeDot";
import { specTabDot } from "../../spec/model/designChanges";
import { useSpecWorkspace } from "../../spec/useSpecWorkspace";

const TabLink = createLink(Tab);

/** A tab's name, with a dot when something waits on the other face; the dot's meaning is read out too. */
function TabLabel({ name, dot }: { name: string; dot: string | null }) {
  return (
    <Box component="span" sx={{ position: "relative", pr: dot ? 1.25 : 0 }} title={dot ?? undefined}>
      {name}
      {dot && (
        <>
          <Box
            component="span"
            aria-hidden
            sx={{ position: "absolute", top: 0, right: 0, width: 7, height: 7, borderRadius: "50%", bgcolor: "primary.main" }}
          />
          <Box component="span" sx={{ position: "absolute", width: 1, height: 1, overflow: "hidden", clip: "rect(0 0 0 0)" }}>
            {` (${dot})`}
          </Box>
        </>
      )}
    </Box>
  );
}

/**
 * Spec · Design · Prototype: the faces of the product's workspace, each a
 * card route of its own. The face not open carries a dot when it has news:
 * the spec, a line a design comment changed; the design, features waiting for
 * it, the prototype, a revision not yet reviewed in this browser.
 */
export function WorkspaceTabs({ active }: { active: "spec" | "design" | "prototype" }) {
  const { projectName } = useParams({ from: "/projects/$projectName" });
  const { model, workspace } = useSpecWorkspace(projectName);
  const specDot =
    active !== "spec" && model.data && specTabDot(model.data.design.specChanges) ? "changed by design feedback" : null;
  const designDot = active !== "design" && workspace?.design.label ? workspace.design.label.toLowerCase() : null;
  const prototypeDot = usePrototypeDot(projectName);
  return (
    <Tabs
      value={active}
      aria-label="Workspace"
      sx={{
        minHeight: 0,
        flex: 1,
        "& .MuiTab-root": { minHeight: 0, py: 0.75, px: 2, textTransform: "none", fontWeight: 600, fontSize: "0.875rem" },
      }}
    >
      <TabLink
        value="spec"
        label={<TabLabel name="Spec" dot={specDot} />}
        to="/projects/$projectName/spec"
        params={{ projectName }}
      />
      <TabLink
        value="design"
        label={<TabLabel name="Design" dot={designDot} />}
        to="/projects/$projectName/design"
        params={{ projectName }}
      />
      <TabLink
        value="prototype"
        label={<TabLabel name="Prototype" dot={active !== "prototype" && prototypeDot ? "a prototype to review" : null} />}
        to="/projects/$projectName/prototype"
        params={{ projectName }}
      />
    </Tabs>
  );
}
