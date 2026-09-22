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
 * What the registry sees of the renderer: the view it draws, a way to render
 * child nodes, and the ONE way to react to a click. Registry files never see
 * `dispatch`; they call `activate` (or spread `pressProps`) and the renderer
 * decides what that means in the current mode — Preview acts, Annotate selects.
 *
 * Selection mechanics (the wrapper rule): while annotating, a selectable
 * element takes pointer events and its content does not, so a click lands on
 * the innermost selectable under the pointer, which stops it there. Table rows,
 * tabs and steps can't be wrapped without breaking their parents' markup, so
 * they take the same behaviour as props (`pressProps`).
 */

import { createContext, useContext, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";
import { Box } from "@wso2/oxygen-ui";
import type { PrototypeAction, PrototypeModelV1, PrototypeNode } from "@aep/prototype-model";
import type { PrototypeViewState } from "../model/viewState";

export interface PrototypeRenderContextValue {
  model: PrototypeModelV1;
  view: PrototypeViewState;
  /** Activate a component: its action in Preview, a selection toggle in Annotate. */
  activate: (componentId: string, action: PrototypeAction) => void;
  /** Toggle a component's selection (Annotate). */
  select: (componentId: string) => void;
  /** Render child nodes through the registry, honouring `showIn`. */
  renderNodes: (nodes: PrototypeNode[]) => ReactNode;
}

export const PrototypeRenderContext = createContext<PrototypeRenderContextValue | null>(null);

export function usePrototypeRender(): PrototypeRenderContextValue {
  const ctx = useContext(PrototypeRenderContext);
  if (!ctx) throw new Error("usePrototypeRender must be used inside PrototypeRenderer");
  return ctx;
}

/** Props that make an element selectable while annotating. */
function annotateProps(id: string, ctx: PrototypeRenderContextValue) {
  const selected = ctx.view.selectedComponentIds.includes(id);
  return {
    role: "button",
    tabIndex: 0,
    "aria-pressed": selected,
    onClick: (e: MouseEvent) => {
      e.stopPropagation();
      ctx.select(id);
    },
    onKeyDown: (e: KeyboardEvent) => {
      if (e.key !== "Enter" && e.key !== " ") return;
      e.preventDefault();
      e.stopPropagation();
      ctx.select(id);
    },
  } as const;
}

/**
 * Props for an element that is itself the interactive thing — a row, a tab, a
 * step — which cannot be wrapped without breaking its parent's markup. Carries
 * the stable model ID; clicking acts in Preview (when there is an action) and
 * selects in Annotate.
 */
export function pressProps(ctx: PrototypeRenderContextValue, id: string, action?: PrototypeAction) {
  if (ctx.view.mode === "annotate") {
    return { "data-prototype-component-id": id, ...annotateProps(id, ctx), sx: { pointerEvents: "auto" } } as const;
  }
  return {
    "data-prototype-component-id": id,
    ...(action ? { onClick: () => ctx.activate(id, action) } : {}),
  } as const;
}

/**
 * Wrap a component so it carries its stable model ID and can be selected in
 * Annotate. Preview behaviour belongs to the content (a Button's onClick).
 */
export function Selectable({ id, inline, children }: { id: string; inline?: boolean; children: ReactNode }) {
  const ctx = usePrototypeRender();
  const annotating = ctx.view.mode === "annotate";
  return (
    <Box
      data-prototype-component-id={id}
      {...(annotating ? annotateProps(id, ctx) : {})}
      sx={{
        display: inline ? "inline-block" : "block",
        minWidth: 0,
        ...(annotating ? { pointerEvents: "auto", cursor: "crosshair" } : {}),
      }}
    >
      <Box sx={{ display: "contents", ...(annotating ? { pointerEvents: "none" } : {}) }}>{children}</Box>
    </Box>
  );
}
