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
 * THE PINNED BLOCK: how a scrolling terminal holds the crew still.
 *
 * The playground is plain line output over readline, not a full-screen
 * application, so the crew view and the step feed merge the way a terminal can
 * carry them — the steps stream as they always have, and the crew sits in a
 * block pinned under them, redrawn in place.
 *
 * **On anything that is not a TTY the block is not drawn at all.** The
 * playground's output is piped and archived — a recorded run is how a tuning
 * change is compared against the one before it — and cursor escapes in a saved
 * file are worse than useless. A non-TTY run therefore emits exactly the tagged
 * lines it emitted before this existed, byte for byte.
 *
 * Redraw discipline, and why each rule is here:
 *
 *   - The block's CONTENT is rebuilt at most once a second. `buildCrew` walks
 *     the whole event array, and a 40-minute run's array is long enough that
 *     rebuilding per line would be quadratic.
 *   - A streamed line erases the block, prints, and repaints what was already
 *     built — so the steps always land ABOVE the block and never inside it.
 *   - A ticker repaints while nothing arrives, because the ages are the point:
 *     a frozen "3s ago" on a wedged run is the precise lie the crew exists to
 *     stop telling.
 *   - Every drawn line is truncated to one column short of the width, so no row
 *     can wrap. A wrapped row makes the block one physical line taller than the
 *     cursor arithmetic believes, and the next erase would eat a line of the
 *     transcript instead. The failure mode is kept at "the block is short", not
 *     "the transcript is mangled".
 */

import { renderCrewBlock, type BlockRow, type CrewBlockOptions } from "./crew-block.js";
import { buildCrew, type RunEventView } from "@aep/progress-view";
import type { AgentTags } from "./agent-tags.js";
import { openPinnedPane, type PaneOutput } from "./pinned-pane.js";

/** At most once a second, per the redraw discipline above. */
const REBUILD_MS = 1000;

export interface CrewPaneOptions {
  out: PaneOutput;
  /**
   * Whether the block may be drawn at all. Passed in rather than read off the
   * stream, so both branches can be driven in a test — a TTY rule nobody can
   * exercise is a TTY rule that breaks the archived transcript one day.
   */
  isTTY: boolean;
  /** This run's agent tags — the SAME registry the streamed step lines use. */
  tag: AgentTags;
  /** The frame loop's clock. The crew model reads none of its own. */
  now?: (() => number) | undefined;
}

/** The surface a coding run writes through. */
export interface CrewPane {
  /** One streamed step line, printed above the block. */
  line(text: string): void;
  /** The events so far. Rebuilds the block, at most once a second. */
  update(events: readonly RunEventView[]): void;
  /** Take the block down, leaving the transcript exactly as it was printed. */
  close(): void;
}

/**
 * Open the crew block over `out`, or a plain printer when there is no terminal
 * to pin it to (./pinned-pane.ts owns both halves of that).
 *
 * This module's own job is WHEN to rebuild: `buildCrew` walks the whole event
 * array, and a 40-minute run's array is long enough that rebuilding per line
 * would be quadratic — so content is rebuilt at most once a second, and a
 * ticker repaints in between because the ages are the point.
 */
export function openCrewPane(opts: CrewPaneOptions): CrewPane {
  const pane = openPinnedPane(opts.out, opts.isTTY);
  const now = opts.now ?? Date.now;
  let builtAt = -Infinity;
  let latest: readonly RunEventView[] = [];

  const rebuild = (): void => {
    const at = now();
    const blockOpts: CrewBlockOptions = { columns: pane.width(), maxRows: pane.height(), tag: opts.tag };
    const content: BlockRow[] = renderCrewBlock(buildCrew(latest, at), at, blockOpts);
    builtAt = at;
    pane.set(content);
  };

  // Ticks the ages while nothing arrives. Unref'd: a run that has otherwise
  // finished must not be held open by the thing drawing its crew.
  const ticker = setInterval(() => {
    if (latest.length > 0) rebuild();
  }, REBUILD_MS);
  ticker.unref();

  return {
    line(text: string): void {
      pane.line(text);
    },
    update(events: readonly RunEventView[]): void {
      latest = events;
      if (now() - builtAt < REBUILD_MS) return;
      rebuild();
    },
    close(): void {
      clearInterval(ticker);
      pane.close();
    },
  };
}
