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
 * The running prototype, as a host page embeds it: `prototype.tsx` executed
 * inside a sandboxed frame, never in the host's own page. The host tells the
 * frame what to run (`load`), what to draw (`view`) and when to start the
 * mock data over (`reset`); the frame answers with navigations, selection
 * toggles, clicks on empty space in Annotate, pin clicks, the elements a
 * screen draws, where the elements a host anchors to are and how far the
 * prototype is scrolled (`onGeometry`, for `useFrameAnchors`), data
 * snapshots, Escape and errors. Through its `ref` a host puts keyboard focus
 * back on an element (or a pin) when its own UI by it closes.
 * Every message's source and shape is checked. Until the app first draws,
 * a loading cover sits over the frame, so an early click is not silently
 * lost; a frame that neither draws nor reports an error within
 * `PROTOTYPE_START_TIMEOUT_MS` says it did not start. Plain React, no theme.
 */

import { useEffect, useImperativeHandle, useMemo, useRef, useState, type ReactNode, type Ref } from "react";
import type { DataSnapshot } from "../data.js";
import type { PrototypeManifest } from "../manifest/types.js";
import { parseFromFrameMessage, type FrameBox, type FrameColorScheme, type FrameElement, type FramePoint, type FrameView, type ToFrameMessage } from "./bridge.js";
import { prototypeFrameDocument } from "./frame-document.js";
import type { FrameGeometry } from "./useFrameAnchors.js";

export interface PrototypeFrameProps {
  /** The prototype's name, for the frame's accessible title (`<title> prototype app`). */
  title: string;
  /** The theme's `frame-runtime.js` text. */
  runtime: string;
  manifest: PrototypeManifest;
  source: string;
  /** Changes exactly when `manifest` or `source` does; a new version reloads the app. */
  version: string;
  view: FrameView;
  /** The snapshot a (re)load starts the mock data from; the seed when absent. */
  initialData?: DataSnapshot | undefined;
  /** Bump to start the mock data from the seed again. */
  resetToken?: number | undefined;
  onNavigate: (screenId: string) => void;
  /** A click in Annotate on an element; `additive` when it held Shift (add to the selection rather than start a new one). */
  onToggle: (elementKey: string, additive: boolean) => void;
  /** A pin was clicked, in either mode: its element and the queued comments' numbers it shows (`[]`: the element's draft pin). */
  onPin?: ((elementKey: string, requests: number[]) => void) | undefined;
  /** A click in Annotate on empty space, on no element: a whole-screen comment at `at`, the spot of the prototype's document (its pin is drawn there). */
  onScreenClick?: ((at: FramePoint) => void) | undefined;
  /** A whole-screen comment's pin was clicked, in either mode: the queued comment's number it shows (`[]`: the screen's hollow pin). */
  onScreenPin?: ((requests: number[]) => void) | undefined;
  onEscape: () => void;
  onElements: (screenId: string, elements: FrameElement[]) => void;
  onData?: ((data: DataSnapshot) => void) | undefined;
  /** Where the toggled, selected and pinned elements are drawn, whenever that changes; give it `useFrameAnchors().onGeometry`. */
  onGeometry?: ((geometry: FrameGeometry) => void) | undefined;
  /**
   * What covers the frame until the prototype first draws (or fails): the
   * runtime is large and takes a moment to start, and a click before then
   * would be lost. A plain "Loading the prototype…" by default.
   */
  loading?: ReactNode;
  /** The host's resolved colour scheme, for the theme to draw the prototype in; the system's when absent. */
  colorScheme?: FrameColorScheme | undefined;
  ref?: Ref<PrototypeFrameHandle> | undefined;
}

/** What a host can ask of the running frame. */
export interface PrototypeFrameHandle {
  /**
   * Move keyboard focus into the frame, onto the element `key`: onto its pin
   * for `requests` when given (`[]`: its draft pin), as `onPin` named it.
   */
  focusElement: (key: string, requests?: readonly number[]) => void;
  /** Move keyboard focus into the frame, onto the whole-screen comment's pin for `requests` (`[]`: the screen's hollow pin), as `onScreenPin` named it. */
  focusScreenPin: (requests: readonly number[]) => void;
}

/** How long the frame may take to draw its app (or report why not) before the host stops waiting. */
export const PROTOTYPE_START_TIMEOUT_MS = 30_000;

const DID_NOT_START = "The prototype didn't start: its runtime did not draw the app or report an error. Close the review and open it again.";

/** The frame document's progress: how many times it said `ready`, and whether it drew (or failed, or stalled) since the last. */
interface Progress {
  readies: number;
  settled: boolean;
}

export function PrototypeFrame(props: PrototypeFrameProps) {
  const { title, runtime, version, resetToken, loading, colorScheme } = props;
  const frame = useRef<HTMLIFrameElement>(null);
  // `readies` counts the frame's `proto:ready` messages: a reloaded frame document is ready again and needs its app re-sent.
  const [{ readies, settled }, setProgress] = useState<Progress>({ readies: 0, settled: false });
  const ready = readies > 0;
  const starting = !settled;
  const [error, setError] = useState<string | null>(null);
  const view = useMemo(() => (colorScheme === undefined ? props.view : { ...props.view, colorScheme }), [props.view, colorScheme]);
  const doc = useMemo(() => prototypeFrameDocument(runtime), [runtime]);

  // The latest props, for the one message listener and the effects below.
  const latest = useRef(props);
  latest.current = props;
  // The boxes and scroll the frame last reported: its geometry replaces them, a toggle adds the clicked element's box at once, a screen click says the scroll.
  const geometry = useRef<{ boxes: Readonly<Record<string, FrameBox>>; scroll: FramePoint }>({ boxes: {}, scroll: { x: 0, y: 0 } });
  const report = (next: Partial<FrameGeometry>) => {
    geometry.current = { boxes: next.boxes ?? geometry.current.boxes, scroll: next.scroll ?? geometry.current.scroll };
    if (frame.current) latest.current.onGeometry?.({ frame: frame.current, ...geometry.current });
  };
  /** A spot the frame named in its viewport (`point`) and its document (`at`): how far the document is scrolled. */
  const scrolledBy = (point: FramePoint, at: FramePoint): FramePoint => ({ x: at.x - point.x, y: at.y - point.y });

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (!frame.current || event.source !== frame.current.contentWindow) return;
      const message = parseFromFrameMessage(event.data);
      if (!message) return;
      const p = latest.current;
      switch (message.type) {
        case "proto:ready":
          setProgress((p) => ({ readies: p.readies + 1, settled: false }));
          break;
        case "proto:navigate":
          p.onNavigate(message.screenId);
          break;
        case "proto:toggle":
          if (message.box) report({ boxes: { ...geometry.current.boxes, [message.elementKey]: message.box } });
          p.onToggle(message.elementKey, message.additive === true);
          break;
        case "proto:pin":
          report({ boxes: { ...geometry.current.boxes, [message.key]: message.box } });
          p.onPin?.(message.key, message.requests);
          break;
        case "proto:screen-click":
          report({ scroll: scrolledBy(message.point, message.at) });
          p.onScreenClick?.(message.at);
          break;
        case "proto:screen-pin":
          report({ scroll: scrolledBy(message.point, message.at) });
          p.onScreenPin?.(message.requests);
          break;
        case "proto:geometry":
          report({ boxes: message.boxes, scroll: message.scroll ?? { x: 0, y: 0 } });
          break;
        case "proto:escape":
          p.onEscape();
          break;
        case "proto:rendered":
          // A report of an earlier version (one the frame drew before it loaded this one) is not what shows.
          if (message.version !== undefined && message.version !== p.version) break;
          setProgress((s) => ({ ...s, settled: true }));
          p.onElements(message.screenId, message.elements);
          break;
        case "proto:data":
          p.onData?.(message.data);
          break;
        case "proto:error":
          setProgress((s) => ({ ...s, settled: true }));
          setError(message.message);
          break;
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

  const post = (message: ToFrameMessage) => {
    // The frame's origin is opaque, so no target origin can be named; what is sent is the prototype and the view, no secret.
    frame.current?.contentWindow?.postMessage(message, "*");
  };

  useImperativeHandle(
    props.ref,
    () => ({
      focusElement: (key, requests) => {
        frame.current?.focus();
        post({ type: "proto:focus", key, ...(requests !== undefined ? { requests: [...requests] } : {}) });
      },
      focusScreenPin: (requests) => {
        frame.current?.focus();
        post({ type: "proto:focus-screen-pin", requests: [...requests] });
      },
    }),
    [],
  );

  // (Re)load the app whenever the frame becomes ready (again) and whenever the prototype changes.
  const loadedVersion = useRef<string | null>(null);
  useEffect(() => {
    if (!ready) return;
    const p = latest.current;
    setError(null);
    loadedVersion.current = version;
    post({ type: "proto:load", source: p.source, manifest: p.manifest, view, data: p.initialData, version });
  }, [readies, version]);

  // A frame that neither draws nor says why within the bound stops being waited for, visibly.
  useEffect(() => {
    if (settled) return;
    const timer = setTimeout(() => {
      setProgress((s) => ({ ...s, settled: true }));
      setError(DID_NOT_START);
    }, PROTOTYPE_START_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [readies, settled, doc]);

  // Draw every later view of the loaded app.
  useEffect(() => {
    if (!ready || loadedVersion.current !== version) return;
    post({ type: "proto:view", view });
    // `version` is read, not a trigger: a new version's first view goes with its load.
  }, [readies, view]);

  // Start the data over when the token changes (not on the first render).
  const lastReset = useRef(resetToken);
  useEffect(() => {
    if (!ready || lastReset.current === resetToken) return;
    lastReset.current = resetToken;
    post({ type: "proto:reset" });
  }, [readies, resetToken]);

  return (
    <div className="proto-frame" style={{ position: "relative", flex: 1, display: "flex", minHeight: 0 }}>
      {error && (
        <div role="alert" className="proto-frame-error" style={{ position: "absolute", top: 8, left: 8, right: 8, zIndex: 1, padding: "8px 12px", borderRadius: 6, background: "#fdecea", color: "#611a15", font: "13px system-ui, sans-serif", display: "flex", gap: 8, alignItems: "flex-start" }}>
          <span style={{ flex: 1 }}>{error}</span>
          <button type="button" onClick={() => setError(null)}>
            Dismiss
          </button>
        </div>
      )}
      {starting && (
        <div role="status" className="proto-frame-loading" style={{ position: "absolute", inset: 0, zIndex: 1, display: "flex", alignItems: "center", justifyContent: "center", background: "#fff" }}>
          {loading ?? <span style={{ font: "13px system-ui, sans-serif", color: "#59636e" }}>Loading the prototype…</span>}
        </div>
      )}
      <iframe ref={frame} aria-busy={starting} title={`${title} prototype app`} sandbox="allow-scripts" srcDoc={doc} style={{ flex: 1, border: 0, width: "100%", height: "100%", display: "block" }} />
    </div>
  );
}
