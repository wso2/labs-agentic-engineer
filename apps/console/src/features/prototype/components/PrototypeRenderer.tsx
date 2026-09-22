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
 * Renders a prototype model's current screen — the application's own shell
 * (its navigation), the screen's nodes, and its overlays — from view state.
 *
 * This is the one place clicks become events: it hands the registry an
 * `activate` that dispatches `ACTIVATE`, and the reducer decides whether that
 * acts (Preview) or selects (Annotate). Every node is wrapped so it carries
 * its stable model ID and can be selected.
 *
 * Only Oxygen components render here: no raw HTML, no markup strings, no
 * style strings — the model has no way to reach any of them.
 */

import { useMemo, type Dispatch } from "react";
import { Stack } from "@wso2/oxygen-ui";
import type { PrototypeModelV1, PrototypeNode } from "@aep/prototype-model";
import { isShownIn } from "../model/visibility";
import type { PrototypeViewEvent, PrototypeViewState } from "../model/viewState";
import { NO_PINS, PrototypeRenderContext, Selectable, type PrototypeRenderContextValue } from "./renderContext";
import { NodeBody } from "./registry";
import { AppShellView } from "./registry/layouts";
import { OverlayView } from "./registry/overlays";

export interface PrototypeRendererProps {
  model: PrototypeModelV1;
  view: PrototypeViewState;
  dispatch: Dispatch<PrototypeViewEvent>;
  /** Queued requests' numbers per component on this screen, pinned while annotating (#817). */
  pins?: ReadonlyMap<string, readonly number[]>;
}

function NodeView({ node, stateId }: { node: PrototypeNode; stateId: string }) {
  if (!isShownIn(node, stateId)) return null;
  return (
    <Selectable id={node.id}>
      <NodeBody node={node} />
    </Selectable>
  );
}

export function PrototypeRenderer({ model, view, dispatch, pins = NO_PINS }: PrototypeRendererProps) {
  const ctx = useMemo<PrototypeRenderContextValue>(
    () => ({
      model,
      view,
      activate: (componentId, action) => dispatch({ type: "ACTIVATE", componentId, action }),
      select: (componentId) => dispatch({ type: "TOGGLE_SELECTION", componentId }),
      renderNodes: (nodes) => nodes.map((n) => <NodeView key={n.id} node={n} stateId={view.stateId} />),
      pins,
    }),
    [model, view, dispatch, pins],
  );
  const screen = model.screens.find((s) => s.id === view.screenId);
  if (!screen) return null;
  const navigation = model.navigation.find((n) => n.id === screen.navigationId);
  const close = () => dispatch({ type: "CLOSE_OVERLAY" });
  return (
    <PrototypeRenderContext.Provider value={ctx}>
      <AppShellView navigation={navigation}>
        <Stack spacing={3}>{ctx.renderNodes(screen.content)}</Stack>
      </AppShellView>
      {(screen.overlays ?? []).map((o) => (
        <OverlayView key={o.id} overlay={o} onClose={close} />
      ))}
    </PrototypeRenderContext.Provider>
  );
}
