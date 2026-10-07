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
 * Selection: the one way a kit component becomes an element a reviewer can
 * point at, and the one place a click is told what it means in the mode. The
 * kit owns it, so Annotate works the same under every theme.
 *
 * Every annotatable element carries its `id` as `data-proto-key` and its
 * on-screen name as `data-proto-label` (the frame reports both to the host;
 * the render check reads the keys to find duplicates), and a navigating
 * element its target as `data-proto-to`.
 *
 *  - `SelectableBox` wraps a component's themed output.
 *  - `selectableRootProps` are spread by a theme on an element that cannot be
 *    wrapped without breaking its parent's markup (a table row, a tab).
 *
 * Annotate mechanics: the scene takes no pointer events; a selectable takes
 * them back and its content does not, so a click lands on the innermost
 * selectable under the pointer. That selectable claims the click in the
 * capture phase and stops it, so no component handler runs; Enter or Space on
 * a focused selectable does the same.
 */

import type { KeyboardEvent, MouseEvent, ReactNode } from "react";
import { useKit, type KitContextValue } from "./context.js";

const EXCERPT = 32;

/** An element's on-screen name, excerpted: display only, never an identity. */
export function excerpt(text: string): string {
  return text.length > EXCERPT ? `${text.slice(0, EXCERPT)}…` : text;
}

/** Throws unless `id` is a non-empty string: every annotatable element needs a stable one. */
export function requireId(component: string, id: unknown): string {
  if (typeof id !== "string" || id === "") {
    throw new Error(`<${component}> needs an id: a stable, unique-per-screen element id a reviewer's request can point at`);
  }
  return id;
}

/**
 * What a theme spreads on an element it cannot wrap (a table row, a tab or
 * step header, a navigation entry). Data attributes and, while annotating,
 * the selection handlers; never a className or style, so the theme's own win.
 */
export interface SelectableRootProps {
  "data-proto-key": string;
  "data-proto-label": string;
  "data-proto-root": "";
  "data-proto-to"?: string | undefined;
  "data-proto-annotating"?: "" | undefined;
  "data-proto-selected"?: "" | undefined;
  "data-proto-pins"?: string | undefined;
  role?: "button" | undefined;
  tabIndex?: number | undefined;
  "aria-pressed"?: boolean | undefined;
  onClickCapture?: ((e: MouseEvent) => void) | undefined;
  onKeyDown?: ((e: KeyboardEvent) => void) | undefined;
}

function insideNestedRoot(e: MouseEvent): boolean {
  const nearest = e.target instanceof Element ? e.target.closest("[data-proto-annotating]") : null;
  return nearest !== null && nearest !== e.currentTarget;
}

function pinsOf(ctx: KitContextValue, key: string): readonly number[] {
  return ctx.view.mode === "annotate" ? (ctx.view.pins[key] ?? []) : [];
}

/**
 * The selection handlers for `key`. A box claims only a click that lands on
 * itself (one inside it landed on a nested selectable); a root claims every
 * click inside it but one on a root nested in it (a table row's actions).
 */
function selectHandlers(ctx: KitContextValue, key: string, claims: "own-target" | "any-inside") {
  return {
    role: "button" as const,
    tabIndex: 0,
    "aria-pressed": ctx.view.selectedKeys.includes(key),
    onClickCapture: (e: MouseEvent) => {
      if (claims === "own-target" && e.target !== e.currentTarget) return;
      if (claims === "any-inside" && insideNestedRoot(e)) return;
      e.stopPropagation();
      e.preventDefault();
      ctx.toggle(key);
    },
    onKeyDown: (e: KeyboardEvent) => {
      if (e.target !== e.currentTarget || (e.key !== "Enter" && e.key !== " ")) return;
      e.preventDefault();
      e.stopPropagation();
      ctx.toggle(key);
    },
  };
}

/** Selection props for a root that cannot be wrapped. A plain function, so a component may call it per item. */
export function selectableRootProps(ctx: KitContextValue, key: string, label: string, to?: string | undefined): SelectableRootProps {
  const base: SelectableRootProps = {
    "data-proto-key": key,
    "data-proto-label": excerpt(label),
    "data-proto-root": "",
    ...(to !== undefined ? { "data-proto-to": to } : {}),
  };
  if (ctx.view.mode !== "annotate") return base;
  const pins = pinsOf(ctx, key);
  return {
    ...base,
    ...selectHandlers(ctx, key, "any-inside"),
    "data-proto-annotating": "",
    ...(ctx.view.selectedKeys.includes(key) ? { "data-proto-selected": "" } : {}),
    ...(pins.length > 0 ? { "data-proto-pins": pins.join(", ") } : {}),
  };
}

export interface SelectableBoxProps {
  id: string;
  label: string;
  inline?: boolean | undefined;
  /** Whether it holds other selectables (their pins take the top-right corner, so its own go top-left). */
  container?: boolean | undefined;
  /** The screen a press on it navigates to, for the render check. */
  to?: string | undefined;
  children: ReactNode;
}

/** A box that carries an element's id and makes it selectable in Annotate: outline, label chip and request pins. */
export function SelectableBox({ id, label, inline = false, container = false, to, children }: SelectableBoxProps) {
  const ctx = useKit();
  const annotating = ctx.view.mode === "annotate";
  const selected = annotating && ctx.view.selectedKeys.includes(id);
  const Tag = inline ? "span" : "div";
  const pins = pinsOf(ctx, id).map((n) => (
    <span key={n} className="proto-chip proto-pin" aria-hidden data-testid="request-pin">
      {n}
    </span>
  ));
  const pinsLeft = pins.length > 0 && container;
  return (
    <Tag
      className={inline ? "proto-selectable proto-inline" : "proto-selectable"}
      data-proto-key={id}
      data-proto-label={excerpt(label)}
      {...(to !== undefined ? { "data-proto-to": to } : {})}
      {...(annotating ? { ...selectHandlers(ctx, id, "own-target"), "data-proto-annotating": "" } : {})}
      {...(selected ? { "data-proto-selected": "" } : {})}
    >
      {(selected || pinsLeft) && (
        <span className="proto-corner proto-corner-left">
          {pinsLeft && pins}
          {selected && (
            <span className="proto-chip proto-label" data-testid="selection-label">
              {excerpt(label)}
            </span>
          )}
        </span>
      )}
      {pins.length > 0 && !pinsLeft && <span className="proto-corner proto-corner-right">{pins}</span>}
      <Tag className="proto-content">{children}</Tag>
    </Tag>
  );
}
