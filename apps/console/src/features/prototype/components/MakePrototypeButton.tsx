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

import { Box, Button, Tooltip } from "@wso2/oxygen-ui";
import type { AppPrototype } from "../model/prototypes";
import { usePrototypes } from "../usePrototypes";
import { usePrototypeTurns } from "../usePrototypeTurns";

/** What the button says for these prototypes: Make before any exists, Update after, and what runs while it runs. */
export function makeLabel(prototypes: readonly AppPrototype[]): string {
  const several = prototypes.length > 1;
  const updating = prototypes.some((p) => p.exists);
  if (prototypes.some((p) => p.status === "revising")) return updating ? "Updating prototype…" : "Making prototype…";
  return `${updating ? "Update" : "Make"} ${several ? "prototypes" : "prototype"}`;
}

/**
 * Make prototype, once the design has a web application: a `/prototype` turn
 * for it (every web application, when there are several), run in the chat.
 * Update prototype once one exists. It waits while a turn runs, and says why
 * unless the turn is its own (the label says that).
 */
export function MakePrototypeButton({
  projectName,
  variant = "outlined",
}: {
  projectName: string;
  variant?: "outlined" | "contained";
}) {
  const prototypes = usePrototypes(projectName);
  const turns = usePrototypeTurns(projectName);
  if (!prototypes || prototypes.length === 0) return null;
  const only = prototypes.length === 1 ? prototypes[0]!.component : undefined;
  const own = prototypes.some((p) => p.status === "revising");
  const button = (
    <Button size="small" variant={variant} disabled={!turns.ready} onClick={() => turns.make(only)}>
      {makeLabel(prototypes)}
    </Button>
  );
  if (!turns.waiting || own) return button;
  // A disabled button gets no pointer events, so the tooltip sits on a wrapper.
  return (
    <Tooltip title={turns.waiting}>
      <Box component="span" sx={{ display: "inline-flex" }}>
        {button}
      </Box>
    </Tooltip>
  );
}
