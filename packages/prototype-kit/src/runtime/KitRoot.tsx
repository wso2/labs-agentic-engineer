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
 * A prototype, drawn: the current screen of a `defineApp` module for the
 * reviewer's view, under a theme. The frame and the render check both render
 * this. The view is the host's (its reducer owns it); KitRoot owns what only
 * the running app can — the params the last navigation carried and the mock
 * data store. Remount it (a new `key`) to start from the seed again.
 */

import { Component, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { PrototypeApp } from "../app.js";
import type { DataSnapshot } from "../data.js";
import type { FramePoint } from "../host/bridge.js";
import type { PrototypeManifest } from "../manifest/types.js";
import type { PrototypeTheme, ThemeRegistry } from "../theme/contract.js";
import { ThemeContext } from "../theme/context.js";
import { KitContext, type KitContextValue, type KitView } from "./context.js";
import { KIT_CSS } from "./kit-css.js";
import { RootPins, ScreenPins } from "./pins.js";
import { watchScreenClicks } from "./screen-click.js";
import { createDataStore } from "./store.js";

export interface KitRootProps {
  app: PrototypeApp;
  manifest: PrototypeManifest;
  theme: PrototypeTheme;
  view: KitView;
  /** A snapshot to start the store from instead of the seed (read once, at mount). */
  initialData?: DataSnapshot | undefined;
  /** A press asked for another screen (Preview only). */
  onNavigate: (screenId: string) => void;
  /** A click selected or deselected an element (Annotate only); `additive` when it held Shift. */
  onToggle: (elementKey: string, additive: boolean) => void;
  /** A pin was clicked (either mode): its element and the queued comments' numbers it shows, none for a draft pin. */
  onPin?: ((elementKey: string, requests: number[]) => void) | undefined;
  /** A click on empty space (Annotate only): where, in the viewport (`point`) and the scrolled document (`at`). */
  onScreenClick?: ((point: FramePoint, at: FramePoint) => void) | undefined;
  /** A whole-screen comment's pin was clicked (either mode): its comment's number (none: the hollow pin), and where it is. */
  onScreenPin?: ((requests: number[], point: FramePoint, at: FramePoint) => void) | undefined;
  /** The mock data changed. */
  onData?: ((snapshot: DataSnapshot) => void) | undefined;
  /** A screen failed to render, or the app asked for a screen that does not exist. */
  onError?: ((message: string) => void) | undefined;
  /** The scheme the theme draws in (the host's); the theme's own choice when absent. */
  colorScheme?: "light" | "dark" | undefined;
}

const NO_PARAMS: Readonly<Record<string, string>> = Object.freeze({});

function Passthrough({ children }: { children: ReactNode }) {
  return <>{children}</>;
}

export function KitRoot({ app, manifest, theme, view, initialData, onNavigate, onToggle, onPin, onScreenClick, onScreenPin, onData, onError, colorScheme }: KitRootProps) {
  const onDataRef = useRef(onData);
  onDataRef.current = onData;
  const onScreenClickRef = useRef(onScreenClick);
  onScreenClickRef.current = onScreenClick;
  const annotating = view.mode === "annotate";
  useEffect(() => (annotating ? watchScreenClicks((point, at) => onScreenClickRef.current?.(point, at)) : undefined), [annotating]);
  const [store] = useState(() => createDataStore(app.data, initialData, (snapshot) => onDataRef.current?.(snapshot)));
  const [lastGo, setLastGo] = useState<{ screen: string; params: Record<string, string> } | null>(null);

  const params = lastGo?.screen === view.screenId ? lastGo.params : NO_PARAMS;
  const ctx = useMemo<KitContextValue>(
    () => ({
      manifest,
      view,
      params,
      store,
      toggle: (key, additive) => {
        if (view.mode === "annotate") onToggle(key, additive);
      },
      openPin: (key, requests) => onPin?.(key, [...requests]),
      go: (screenId, next = {}) => {
        if (view.mode === "annotate") return;
        if (!manifest.screens.some((s) => s.id === screenId)) {
          onError?.(`navigation to ${JSON.stringify(screenId)}, which is not one of the prototype's screens`);
          return;
        }
        setLastGo({ screen: screenId, params: next });
        onNavigate(screenId);
      },
    }),
    [manifest, view, params, store, onNavigate, onToggle, onPin, onError],
  );

  const Screen = app.screens[view.screenId];
  const Provider = theme.Provider ?? Passthrough;
  return (
    <ThemeContext.Provider value={theme.registry}>
      <KitContext.Provider value={ctx}>
        <Provider colorScheme={colorScheme}>
          <style>{KIT_CSS}</style>
          <div className="proto-scene" data-proto-mode={view.mode}>
            {Screen ? (
              <ScreenBoundary key={view.screenId} screenId={view.screenId} registry={theme.registry} onError={onError}>
                <Screen />
              </ScreenBoundary>
            ) : (
              <theme.registry.Alert tone="warning" title="Screen not drawn" text={`prototype.tsx has no screen ${JSON.stringify(view.screenId)} although prototype.json lists it.`} />
            )}
            <RootPins />
            {view.screenPins && view.screenPins.length > 0 && <ScreenPins pins={view.screenPins} onOpen={(requests, point, at) => onScreenPin?.(requests, point, at)} />}
          </div>
        </Provider>
      </KitContext.Provider>
    </ThemeContext.Provider>
  );
}

/** One screen failing shows its error instead of taking the whole prototype down. */
class ScreenBoundary extends Component<
  { screenId: string; registry: ThemeRegistry; onError: ((message: string) => void) | undefined; children: ReactNode },
  { error: string | null }
> {
  override state = { error: null as string | null };

  static getDerivedStateFromError(error: unknown) {
    return { error: error instanceof Error ? error.message : String(error) };
  }

  override componentDidCatch(error: unknown) {
    this.props.onError?.(`${this.props.screenId}: ${error instanceof Error ? error.message : String(error)}`);
  }

  override render() {
    if (this.state.error === null) return this.props.children;
    const Alert = this.props.registry.Alert;
    return <Alert tone="error" title="This screen failed to render" text={this.state.error} />;
  }
}
