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
 * The application window a prototype renders in: window chrome, the
 * prototype's name and a read-only address bar showing where the review is,
 * so where the prototype ends and the host begins is never in doubt. The
 * address is `prototype://<screenId>`, with `?flow=…&state=…` when the view
 * is in a flow or in a display state other than the manifest's first. Plain
 * React, no theme: the structure is inline, the look is CSS variables
 * (`--proto-window-*`, each with a neutral default) and the stable classes
 * `proto-window`, `-bar`, `-dots`, `-title`, `-address`, `-body` the host
 * styles as it likes.
 */

import type { CSSProperties, ReactNode } from "react";
import type { PrototypeManifest } from "../manifest/types.js";
import type { PrototypeViewState } from "./view-state.js";

export interface PrototypeWindowProps {
  /** The prototype's name, shown in the bar. */
  title: string;
  manifest: PrototypeManifest;
  /** The review's view; only the screen, flow and state show in the address. */
  view: Pick<PrototypeViewState, "screenId" | "flowId" | "stateId">;
  /** Extra class on the window, for the host's styling. */
  className?: string | undefined;
  /** Extra style on the window; set `--proto-window-*` here. */
  style?: CSSProperties | undefined;
  /** Usually the `PrototypeFrame`. */
  children: ReactNode;
}

/** The address a view shows: `prototype://<screenId>`, plus the flow and the display state when they are not the defaults. */
export function prototypeAddress(manifest: PrototypeManifest, view: Pick<PrototypeViewState, "screenId" | "flowId" | "stateId">): string {
  const params = new URLSearchParams();
  if (view.flowId !== null) params.set("flow", view.flowId);
  if (view.stateId !== "" && view.stateId !== manifest.states[0]?.id) params.set("state", view.stateId);
  const query = params.toString();
  return `prototype://${view.screenId}${query === "" ? "" : `?${query}`}`;
}

const v = (name: string, fallback: string) => `var(--proto-window-${name}, ${fallback})`;

export function PrototypeWindow({ title, manifest, view, className, style, children }: PrototypeWindowProps) {
  return (
    <section
      aria-label={`${title} prototype`}
      className={className ? `proto-window ${className}` : "proto-window"}
      style={{
        flex: 1,
        minWidth: 0,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        border: `1px solid ${v("border", "#c9d0d9")}`,
        borderRadius: v("radius", "10px"),
        background: v("bg", "#fff"),
        boxShadow: v("shadow", "0 8px 24px rgba(15,23,42,.12)"),
        ...style,
      }}
    >
      <div
        className="proto-window-bar"
        style={{ display: "flex", gap: 12, alignItems: "center", padding: "6px 12px", background: v("bar-bg", "#f1f3f6"), borderBottom: `1px solid ${v("bar-border", "#d5dae1")}`, color: v("fg", "inherit") }}
      >
        <span className="proto-window-dots" aria-hidden style={{ display: "flex", gap: 6 }}>
          {[0, 1, 2].map((i) => (
            <i key={i} style={{ width: 10, height: 10, borderRadius: "50%", background: v("dot", "#d0d5dc") }} />
          ))}
        </span>
        <span className="proto-window-title" style={{ fontWeight: 600 }}>
          {title}
        </span>
        <span
          className="proto-window-address"
          aria-label="Address"
          style={{ flex: 1, minWidth: 0, padding: "2px 10px", borderRadius: 6, background: v("address-bg", "#fff"), color: v("address-fg", "#59636e"), fontFamily: "ui-monospace, monospace", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}
        >
          {prototypeAddress(manifest, view)}
        </span>
      </div>
      <div className="proto-window-body" style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
        {children}
      </div>
    </section>
  );
}
