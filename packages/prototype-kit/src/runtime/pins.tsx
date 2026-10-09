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
 * Comment pins: the numbered pins of the queued comments on an element and
 * the hollow pin of its draft, drawn in both modes. Each is a button named by
 * its comment, so a reviewer can reach it with the keyboard; pressing it opens
 * the comment (the host draws the bubble). A box draws its pins in its corner
 * (`SelectableBox`); an element that cannot be wrapped (a table row, a tab)
 * has its pins drawn over its top-right corner by `RootPins`.
 */

import { useEffect, useState, type MouseEvent } from "react";
import type { FrameBox, FramePoint, FrameScreenPin } from "../host/bridge.js";
import { useKit, type KitContextValue } from "./context.js";
import { watchGeometry } from "./geometry.js";

/** Whether `key` carries a queued comment's pin or a draft pin. */
export function hasPins(ctx: KitContextValue, key: string): boolean {
  return (ctx.view.pins[key]?.length ?? 0) > 0 || (ctx.view.drafts?.includes(key) ?? false);
}

/** The pin buttons of `elementKey`: one per queued comment on it, then its draft's. */
export function Pins({ elementKey }: { elementKey: string }) {
  const ctx = useKit();
  const open = (requests: readonly number[]) => (e: MouseEvent) => {
    // The pin's own: no selection, press or navigation under it.
    e.stopPropagation();
    e.preventDefault();
    ctx.openPin(elementKey, requests);
  };
  return (
    <>
      {(ctx.view.pins[elementKey] ?? []).map((n) => (
        <button
          key={n}
          type="button"
          className="proto-chip proto-pin"
          aria-label={`Comment ${n}`}
          data-proto-pin={n}
          data-proto-pin-for={elementKey}
          data-testid="request-pin"
          onClick={open([n])}
        >
          {n}
        </button>
      ))}
      {ctx.view.drafts?.includes(elementKey) && (
        <button
          type="button"
          className="proto-chip proto-pin proto-pin-draft"
          aria-label="Draft comment"
          data-proto-pin="draft"
          data-proto-pin-for={elementKey}
          data-testid="draft-pin"
          onClick={open([])}
        >
          …
        </button>
      )}
    </>
  );
}

const rootSelector = (key: string) => `[data-proto-root][data-proto-key="${CSS.escape(key)}"]`;

/**
 * The pins of the elements a theme drew from `selectableRootProps`: the kit
 * cannot put children in them (a table row takes only cells), so it draws
 * their pins over their top-right corner and keeps them there as the screen
 * scrolls, resizes or redraws.
 *
 * @knipkeep drawn by `KitRoot`, which only the theme runtime bundles reach (their entries are not knip's)
 */
export function RootPins() {
  const ctx = useKit();
  const pinned = [...Object.keys(ctx.view.pins), ...(ctx.view.drafts ?? [])];
  const signature = [...new Set(pinned)].join("\n");
  const [boxes, setBoxes] = useState<Record<string, FrameBox>>({});

  useEffect(() => {
    const keys = signature === "" ? [] : signature.split("\n");
    if (keys.length === 0) {
      setBoxes({});
      return;
    }
    const watcher = watchGeometry(() => keys.filter((k) => document.querySelector(rootSelector(k)) !== null), setBoxes);
    watcher.refresh();
    return watcher.stop;
  }, [signature]);

  return (
    <>
      {Object.entries(boxes).map(([key, box]) => (
        <span key={key} className="proto-root-pins" style={{ top: box.y + 2, left: box.x + box.width - 2 }}>
          <Pins elementKey={key} />
        </span>
      ))}
    </>
  );
}

/**
 * Whole-screen comments' pins, each at the spot of the document where it was
 * made, so it scrolls with the page: numbered for a queued comment, hollow
 * for the screen's draft or the comment being written there. A pin is a
 * button in either mode; clicking it reports its comment's number (none:
 * the hollow pin) and where it is.
 */
export function ScreenPins({ pins, onOpen }: { pins: readonly FrameScreenPin[]; onOpen: (requests: number[], point: FramePoint, at: FramePoint) => void }) {
  const open = (pin: FrameScreenPin) => (e: MouseEvent<HTMLButtonElement>) => {
    // The pin's own: no selection, press or screen click under it.
    e.stopPropagation();
    e.preventDefault();
    const r = e.currentTarget.getBoundingClientRect();
    onOpen(pin.number === undefined ? [] : [pin.number], { x: r.x + r.width / 2, y: r.y + r.height / 2 }, pin.at);
  };
  return (
    <>
      {pins.map((pin) => (
        <button
          key={pin.number ?? "draft"}
          type="button"
          className={pin.number === undefined ? "proto-chip proto-pin proto-screen-pin proto-pin-draft" : "proto-chip proto-pin proto-screen-pin"}
          style={{ left: pin.at.x, top: pin.at.y }}
          aria-label={pin.number === undefined ? "Draft comment on the screen" : `Comment ${pin.number}`}
          data-proto-screen-pin={pin.number ?? "draft"}
          data-testid={pin.number === undefined ? "draft-pin" : "request-pin"}
          onClick={open(pin)}
        >
          {pin.number ?? "…"}
        </button>
      ))}
    </>
  );
}
