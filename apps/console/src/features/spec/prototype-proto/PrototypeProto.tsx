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

// PROTOTYPE (throwaway, issue #813).
// Three variants of the Preview/Annotate prototype shell, switchable via
// `?variant=A|B|C` on the existing /projects/$projectName/prototype/$component
// route, rendering the expense-approval fixture in memory. Question answered:
// "What should the prototype review page look like, and does the v1 registry
// cover an enterprise approval app?"

import { useEffect, useReducer, useState } from "react";
import { Box, Collapse, PageContent, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { PrototypeSwitcher } from "../../../components/PrototypeSwitcher";
import { expenseApproval } from "./fixture";
import { RenderProvider } from "./Renderer";
import { VariantA, name as nameA } from "./VariantA";
import { VariantB, name as nameB } from "./VariantB";
import { VariantC, name as nameC } from "./VariantC";
import { VariantD, name as nameD } from "./VariantD";
import { VariantE, name as nameE } from "./VariantE";
import { VariantF, name as nameF } from "./VariantF";
import { initialState, reduce } from "./viewState";

export const VARIANTS = ["A", "B", "C", "D", "E", "F"] as const;
export type VariantKey = (typeof VARIANTS)[number];

export function PrototypeProto({ variant, onVariantChange }: { variant: VariantKey; onVariantChange: (v: VariantKey) => void }) {
  const model = expenseApproval;
  const [state, dispatch] = useReducer(reduce, initialState(model.roles[0]!.id, model.defaultScreenId, model.states[0]!.id));
  const [showState, setShowState] = useState(false);

  // Escape clears selection; `s` toggles the state dump.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA")) return;
      if (e.key === "Escape") dispatch({ type: "CLEAR_SELECTION" });
      if (e.key === "s") setShowState((v) => !v);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const props = { model, state, dispatch };
  const names = { A: nameA, B: nameB, C: nameC, D: nameD, E: nameE, F: nameF };
  return (
    <RenderProvider value={props}>
      <PageContent fullWidth noPadding sx={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
        {variant === "A" && <VariantA {...props} />}
        {variant === "B" && <VariantB {...props} />}
        {variant === "C" && <VariantC {...props} />}
        {variant === "D" && <VariantD {...props} />}
        {variant === "E" && <VariantE {...props} />}
        {variant === "F" && <VariantF {...props} />}
      </PageContent>
      <PrototypeSwitcher variants={VARIANTS} current={variant} names={names} onChange={(v) => onVariantChange(v as VariantKey)} />
      {/* State surface (press `s`) */}
      <Collapse in={showState}>
        <Paper elevation={12} sx={{ position: "fixed", bottom: 60, right: 16, width: 360, maxHeight: "50vh", overflow: "auto", p: 1.5, zIndex: (t) => t.zIndex.tooltip + 1 }}>
          <Stack spacing={0.5}>
            <Typography variant="overline">view state</Typography>
            <Box component="pre" sx={{ m: 0, fontSize: 11, fontFamily: "monospace", whiteSpace: "pre-wrap" }}>
              {JSON.stringify({ ...state, queue: state.queue.length }, null, 2)}
            </Box>
          </Stack>
        </Paper>
      </Collapse>
    </RenderProvider>
  );
}
