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
import { Box, Chip, Stack } from "@wso2/oxygen-ui";
import type { PrototypeAction, PrototypeModelV1, PrototypeNode } from "@aep/prototype-model";
import { labelOf } from "../model/labels";
import type { PrototypeViewState } from "../model/viewState";

/** No queued requests: what a renderer outside Annotate review shows. */
export const NO_PINS: ReadonlyMap<string, readonly number[]> = new Map();

export interface PrototypeRenderContextValue {
  model: PrototypeModelV1;
  view: PrototypeViewState;
  /** Activate a component: its action in Preview, a selection toggle in Annotate. */
  activate: (componentId: string, action: PrototypeAction) => void;
  /** Toggle a component's selection (Annotate). */
  select: (componentId: string) => void;
  /** Render child nodes through the registry, honouring `showIn`. */
  renderNodes: (nodes: PrototypeNode[]) => ReactNode;
  /**
   * The queued requests' numbers per component on this screen (#817), shown
   * as pins while annotating. Empty outside Annotate.
   */
  pins: ReadonlyMap<string, readonly number[]>;
}

/** The pins a component shows: its queued requests' numbers, while annotating. */
function pinsOf(ctx: PrototypeRenderContextValue, id: string): readonly number[] {
  return ctx.view.mode === "annotate" ? (ctx.pins.get(id) ?? []) : [];
}

/** The outline an annotating element draws: solid when selected, a hint on hover. */
function annotateOutline(selected: boolean) {
  return {
    cursor: "crosshair",
    outline: "2px solid",
    outlineOffset: 2,
    outlineColor: selected ? "primary.main" : "transparent",
    borderRadius: 1,
    "&:hover": { outlineColor: selected ? "primary.main" : "primary.light" },
  } as const;
}

/** A pseudo-element badge for elements that cannot hold a child (rows, tabs, steps). */
function pinBadge(pins: readonly number[]) {
  if (pins.length === 0) return {};
  return {
    position: "relative",
    "&::after": {
      content: `"${pins.join(", ")}"`,
      position: "absolute",
      top: 2,
      right: 2,
      px: 0.75,
      borderRadius: 2,
      bgcolor: "warning.main",
      color: "warning.contrastText",
      typography: "caption",
      fontWeight: "fontWeightMedium",
      pointerEvents: "none",
      zIndex: 2,
    },
  } as const;
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
    const selected = ctx.view.selectedComponentIds.includes(id);
    return {
      "data-prototype-component-id": id,
      ...annotateProps(id, ctx),
      sx: { pointerEvents: "auto", ...annotateOutline(selected), ...pinBadge(pinsOf(ctx, id)) },
    } as const;
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
  const selected = annotating && ctx.view.selectedComponentIds.includes(id);
  const pins = pinsOf(ctx, id);
  return (
    <Box
      data-prototype-component-id={id}
      {...(annotating ? annotateProps(id, ctx) : {})}
      sx={{
        position: "relative",
        display: inline ? "inline-block" : "block",
        minWidth: 0,
        ...(annotating ? { pointerEvents: "auto", ...annotateOutline(selected) } : {}),
      }}
    >
      {selected && (
        <Chip
          size="small"
          color="primary"
          label={labelOf(ctx.model, ctx.view.screenId, id)}
          data-testid="selection-label"
          sx={{ position: "absolute", top: -12, left: 4, zIndex: 3, pointerEvents: "none", maxWidth: 240 }}
        />
      )}
      {pins.length > 0 && (
        <Stack
          direction="row"
          spacing={0.5}
          aria-hidden
          sx={{ position: "absolute", top: -10, right: -6, zIndex: 3, pointerEvents: "none" }}
        >
          {pins.map((n) => (
            <Chip key={n} size="small" color="warning" label={n} data-testid="request-pin" sx={{ minWidth: 24 }} />
          ))}
        </Stack>
      )}
      <Box sx={{ display: "contents", ...(annotating ? { pointerEvents: "none" } : {}) }}>{children}</Box>
    </Box>
  );
}
