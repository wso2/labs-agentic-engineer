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

/** At most once a second, per the redraw discipline above. */
const REBUILD_MS = 1000;

/** What a terminal is assumed to be when it will not say. */
const FALLBACK_COLUMNS = 100;
const FALLBACK_ROWS = 30;

/** Cursor up N lines, then erase from the cursor to the end of the screen. */
const up = (n: number): string => `\x1b[${String(n)}A`;
const ERASE_BELOW = "\x1b[0J";

/** Semantic tone → SGR code. The block never picks a colour by hand. */
const TONE_CODES: Record<string, string> = {
  muted: "2",
  info: "36",
  success: "32",
  warn: "33",
  error: "31",
};

/** The write sink — `process.stdout`, or a buffer in a test. */
export interface PaneOutput {
  write(chunk: string): unknown;
  columns?: number | undefined;
  rows?: number | undefined;
}

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
 * A pane that only prints. What a piped run gets, and what the whole harness got
 * before the block existed.
 */
function plainPane(out: PaneOutput): CrewPane {
  return {
    line(text: string): void {
      out.write(`${text}\n`);
    },
    update(): void {
      // Nothing is pinned, so there is nothing to repaint.
    },
    close(): void {
      // and nothing to take down.
    },
  };
}

/**
 * Open the crew block over `out`, or a plain printer when there is no terminal
 * to pin it to.
 *
 * The returned pane OWNS every write to `out` for the life of the run: a write
 * that goes around it lands inside the block and the next erase takes the
 * transcript with it.
 */
export function openCrewPane(opts: CrewPaneOptions): CrewPane {
  if (!opts.isTTY) return plainPane(opts.out);

  const out = opts.out;
  const now = opts.now ?? Date.now;
  /** What the block should say. */
  let content: BlockRow[] = [];
  /** What is actually on screen right now — the cursor arithmetic's only input. */
  let drawn = 0;
  let builtAt = -Infinity;
  let latest: readonly RunEventView[] = [];

  const paint = (row: BlockRow): string => {
    const code = TONE_CODES[row.tone];
    return code ? `\x1b[${code}m${row.text}\x1b[0m\n` : `${row.text}\n`;
  };

  const clear = (): void => {
    if (drawn === 0) return;
    out.write(up(drawn) + ERASE_BELOW);
    drawn = 0;
  };

  const draw = (): void => {
    if (content.length === 0) return;
    out.write(content.map(paint).join(""));
    drawn = content.length;
  };

  const rebuild = (): void => {
    const at = now();
    const blockOpts: CrewBlockOptions = {
      columns: out.columns ?? FALLBACK_COLUMNS,
      // Two lines of headroom: the shell's own prompt has to fit under the block
      // when the run ends, and a block exactly as tall as the screen scrolls
      // itself off the top the moment anything else is printed.
      maxRows: Math.max(3, (out.rows ?? FALLBACK_ROWS) - 2),
      tag: opts.tag,
    };
    content = renderCrewBlock(buildCrew(latest, at), at, blockOpts);
    builtAt = at;
    clear();
    draw();
  };

  // Ticks the ages while nothing arrives. Unref'd: a run that has otherwise
  // finished must not be held open by the thing drawing its crew.
  const ticker = setInterval(() => {
    if (latest.length > 0) rebuild();
  }, REBUILD_MS);
  ticker.unref();

  return {
    line(text: string): void {
      clear();
      out.write(`${text}\n`);
      draw();
    },
    update(events: readonly RunEventView[]): void {
      latest = events;
      if (now() - builtAt < REBUILD_MS) return;
      rebuild();
    },
    close(): void {
      clearInterval(ticker);
      clear();
      content = [];
    },
  };
}
