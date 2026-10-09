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
 * The frame runtime's entry, bundled per theme by `buildThemeRuntimes`: the
 * script inside a host's sandboxed prototype frame. It waits for `load`, runs
 * the prototype, draws the view it is sent, and reports back what the screen
 * holds, what the reviewer pressed (an element, a pin, empty space in
 * Annotate), the mock data and anything that failed. Asked, it puts focus
 * back on an element or a pin.
 */

import { Component, useEffect, useRef, useState, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import type { PrototypeApp } from "../app.js";
import type { DataSnapshot } from "../data.js";
import { parseToFrameMessage, type FrameElement, type FrameView, type FromFrameMessage } from "../host/bridge.js";
import type { PrototypeManifest } from "../manifest/types.js";
import { KitRoot } from "../runtime/KitRoot.js";
import { moduleFactorySource } from "../runtime/module-source.js";
import { runPrototypeModule, type ModuleFactory } from "../runtime/modules.js";
import { transpileSource } from "../source/transpile.js";
import type { PrototypeTheme } from "../theme/contract.js";
import { boxOf, watchGeometry } from "../runtime/geometry.js";

function post(message: FromFrameMessage) {
  // The parent is the host page; the frame's own origin is opaque, so no target origin can be named. Nothing sent is a secret.
  window.parent.postMessage(message, "*");
}

/** Focus the pin `proto:pin` named (`requests`; `[]`: the draft pin) on `key`, else the element itself when it takes focus (Annotate). */
function focusElement(key: string, requests: readonly number[] | undefined) {
  const of = CSS.escape(key);
  const pin = requests && document.querySelector(`[data-proto-pin-for="${of}"][data-proto-pin="${requests.length > 0 ? String(requests[0]) : "draft"}"]`);
  const target = pin || document.querySelector(`[data-proto-key="${of}"]`);
  if (target instanceof HTMLElement) target.focus();
}

/** Focus the whole-screen comment's pin `proto:screen-pin` named (`requests`; `[]`: the hollow pin). */
function focusScreenPin(requests: readonly number[]) {
  const pin = document.querySelector(`[data-proto-screen-pin="${requests.length > 0 ? String(requests[0]) : "draft"}"]`);
  if (pin instanceof HTMLElement) pin.focus();
}

/** Whether the key is the prototype's own: a control inside it closed a picker or an overlay with it, so the host must not act on it. */
function prototypeUsedEscape(e: KeyboardEvent): boolean {
  if (e.defaultPrevented) return true;
  const t = e.target;
  return t instanceof HTMLElement && (t.isContentEditable || t.matches("input, select, textarea"));
}

interface Loaded {
  app: PrototypeApp;
  manifest: PrototypeManifest;
  initialData: DataSnapshot | undefined;
  /** The version the host named this prototype (none from a host on the older protocol), echoed on each report of what it drew. */
  version: string | undefined;
  /** Bumped per load and reset, so the app remounts with a fresh store. */
  generation: number;
}

let generation = 0;

/**
 * Runs the prototype. The manifest is the host's, already parsed: a host hands
 * `PrototypeFrame` a `PrototypeManifest`, so the frame does not parse it again
 * (which keeps the manifest schema's validator out of this runtime).
 */
function load(source: string, manifest: PrototypeManifest, data: DataSnapshot | undefined, version: string | undefined): Loaded {
  const transpiled = transpileSource(source);
  if (!transpiled.ok) throw new Error(transpiled.findings.map((f) => `${f.location}: ${f.message}`).join("; "));
  // The frame is the sandbox: evaluating the module here is what it is for.
  const factory = (0, eval)(moduleFactorySource(transpiled.code)) as ModuleFactory;
  return { app: runPrototypeModule(factory), manifest, initialData: data, version, generation: ++generation };
}

/** The elements the document draws now, in document order, each once. */
function drawnElements(): FrameElement[] {
  const seen = new Set<string>();
  const out: FrameElement[] = [];
  for (const el of document.querySelectorAll<HTMLElement>("[data-proto-key]")) {
    const key = el.dataset["protoKey"];
    if (!key || seen.has(key)) continue;
    seen.add(key);
    out.push({ key, label: el.dataset["protoLabel"] ?? key });
  }
  return out;
}

/** What is drawn: the loaded prototype's version and the screen showing (none before an app is loaded). */
interface Drawing {
  version: string | undefined;
  screenId: string;
}

/** Reports the drawn elements whenever they (or the version drawing them) change, at most once a frame. */
function watchElements(drawing: () => Drawing | undefined): { report: () => void; stop: () => void } {
  let last = "";
  // The animation frame a report waits for; null when none is pending.
  let pending: number | null = null;
  const report = () => {
    pending = null;
    const now = drawing();
    if (now === undefined) return;
    const elements = drawnElements();
    const signature = `${now.version ?? ""}\n${now.screenId}\n${elements.map((e) => `${e.key}\t${e.label}`).join("\n")}`;
    if (signature === last) return;
    last = signature;
    post({ type: "proto:rendered", ...(now.version !== undefined ? { version: now.version } : {}), screenId: now.screenId, elements });
  };
  const observer = new MutationObserver(() => {
    if (pending === null) pending = requestAnimationFrame(report);
  });
  observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["data-proto-key", "data-proto-label"] });
  return {
    report,
    stop: () => {
      observer.disconnect();
      if (pending !== null) cancelAnimationFrame(pending);
      pending = null;
    },
  };
}

function Frame({ theme }: { theme: PrototypeTheme }) {
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [view, setView] = useState<FrameView | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const drawing = useRef<Drawing | undefined>(undefined);
  // Nothing has drawn until an app is loaded: a view sent before it must not report a draw (that clears the host's loading cover).
  drawing.current = loaded && view ? { version: loaded.version, screenId: view.screenId } : undefined;
  const watcher = useRef<ReturnType<typeof watchElements> | null>(null);
  // The elements the host anchors its UI to: the selected, the pinned and the drafted ones.
  const anchored = useRef<readonly string[]>([]);
  anchored.current = loaded && view ? [...view.selectedKeys, ...Object.keys(view.pins), ...(view.drafts ?? [])] : [];
  const geometry = useRef<ReturnType<typeof watchGeometry> | null>(null);

  useEffect(() => {
    watcher.current = watchElements(() => drawing.current);
    return () => watcher.current?.stop();
  }, []);

  // Where things are is reported only once an app draws: a frame with none has nothing to anchor to.
  const hasApp = loaded !== null;
  useEffect(() => {
    if (!hasApp) return;
    const watch = watchGeometry(
      () => anchored.current,
      (boxes, scroll) => post({ type: "proto:geometry", boxes, scroll }),
    );
    geometry.current = watch;
    return () => {
      watch.stop();
      geometry.current = null;
    };
  }, [hasApp]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (event.source !== window.parent) return;
      const message = parseToFrameMessage(event.data);
      if (!message) return;
      if (message.type === "proto:focus") {
        focusElement(message.key, message.requests);
        return;
      }
      if (message.type === "proto:focus-screen-pin") {
        focusScreenPin(message.requests);
        return;
      }
      if (message.type === "proto:reset") {
        setLoaded((l) => l && { ...l, initialData: undefined, generation: ++generation });
        return;
      }
      if (message.type === "proto:load") {
        try {
          setLoaded(load(message.source, message.manifest, message.data, message.version));
          setFailure(null);
        } catch (e) {
          const text = e instanceof Error ? e.message : String(e);
          setLoaded(null);
          setFailure(text);
          post({ type: "proto:error", message: text });
        }
      }
      setView(message.view);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !prototypeUsedEscape(e)) post({ type: "proto:escape" });
    };
    window.addEventListener("message", onMessage);
    window.addEventListener("keydown", onKey);
    post({ type: "proto:ready" });
    return () => {
      window.removeEventListener("message", onMessage);
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  // A screen change the DOM does not show (the same elements) still reports.
  useEffect(() => {
    const frame = requestAnimationFrame(() => watcher.current?.report());
    return () => cancelAnimationFrame(frame);
  }, [view?.screenId, loaded]);

  // Another selection or other pins: measure what the host now anchors to.
  useEffect(() => geometry.current?.refresh(), [view, loaded]);

  if (failure) return <p role="alert">{failure}</p>;
  if (!loaded || !view) return null;
  return (
    <FrameBoundary key={loaded.generation}>
      <KitRoot
        app={loaded.app}
        manifest={loaded.manifest}
        theme={theme}
        view={view}
        initialData={loaded.initialData}
        onNavigate={(screenId) => post({ type: "proto:navigate", screenId })}
        onToggle={(elementKey, additive) => post({ type: "proto:toggle", elementKey, box: boxOf(elementKey), additive })}
        onPin={(key, requests) => {
          const box = boxOf(key);
          if (box) post({ type: "proto:pin", key, requests, box });
        }}
        onScreenClick={(point, at) => post({ type: "proto:screen-click", point, at })}
        onScreenPin={(requests, point, at) => post({ type: "proto:screen-pin", requests, point, at })}
        onData={(data) => post({ type: "proto:data", data })}
        onError={(message) => post({ type: "proto:error", message })}
        colorScheme={view.colorScheme}
      />
    </FrameBoundary>
  );
}

/**
 * What a screen's own boundary cannot catch (the theme's Provider throwing,
 * the kit root itself) is reported to the host, which would otherwise wait
 * on its loading cover, and said in the frame.
 */
class FrameBoundary extends Component<{ children: ReactNode }, { error: string | null }> {
  override state = { error: null as string | null };

  static getDerivedStateFromError(error: unknown) {
    return { error: error instanceof Error ? error.message : String(error) };
  }

  override componentDidCatch(error: unknown) {
    post({ type: "proto:error", message: `The prototype failed to draw: ${error instanceof Error ? error.message : String(error)}` });
  }

  override render() {
    return this.state.error === null ? this.props.children : <p role="alert">{this.state.error}</p>;
  }
}

export function startFrame(theme: PrototypeTheme): void {
  const root = document.getElementById("root");
  if (!root) throw new Error("the prototype frame document has no #root");
  createRoot(root).render(<Frame theme={theme} />);
}
