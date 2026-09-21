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
 * HOW A SCROLLING TERMINAL HOLDS A BLOCK STILL.
 *
 * The mechanism only: a block of rows pinned under the output, erased and
 * repainted around every streamed line, taken down on close. WHAT the rows say
 * is the caller's business — the crew of a coding run (./crew-pane.ts), the
 * services of a wired session (./wire/panel.ts).
 *
 * The rules are the ones the crew block was written with, and each is a bug
 * that happened:
 *
 *   - **On anything that is not a TTY the block is not drawn at all.** The
 *     playground's output is piped and archived, and cursor escapes in a saved
 *     file are worse than useless. A non-TTY run emits exactly the lines it
 *     would have emitted with no block, byte for byte.
 *   - A streamed line erases the block, prints, and repaints — so lines always
 *     land ABOVE the block and never inside it.
 *   - The pane OWNS every write to `out` for its lifetime. A write that goes
 *     around it lands inside the block and the next erase takes the transcript
 *     with it.
 *   - Rows arrive already truncated to the width. A wrapped row makes the block
 *     one physical line taller than the cursor arithmetic believes, and the
 *     next erase eats a line of the transcript instead.
 */

import type { LineTone } from "@aep/progress-view";

/** One drawn line: its text, and the semantic weight the pane colours it by. */
export interface PaneRow {
  text: string;
  tone: LineTone;
}

/** The write sink — `process.stdout`, or a buffer in a test. */
export interface PaneOutput {
  write(chunk: string): unknown;
  columns?: number | undefined;
  rows?: number | undefined;
}

/** What a terminal is assumed to be when it will not say. */
const FALLBACK_COLUMNS = 100;
const FALLBACK_ROWS = 30;

/**
 * The widest a row may be: one column short of the terminal, and never under 20.
 *
 * One short because a row that reaches the last column wraps, and a wrapped row
 * makes the block one physical line taller than the cursor arithmetic believes —
 * the next erase then eats a line of the transcript. The floor covers a terminal
 * that reports 0 columns, which is what a pty opened without a window size does.
 */
export function drawWidth(columns: number | undefined): number {
  return Math.max(20, (columns ?? FALLBACK_COLUMNS) - 1);
}

/** Cut to width, saying so — a silently cut line reads as a line that ended. */
export function fit(text: string, width: number): string {
  if (width <= 0) return "";
  return text.length <= width ? text : `${text.slice(0, Math.max(0, width - 1))}…`;
}

/** Cursor up N lines, then erase from the cursor to the end of the screen. */
const up = (n: number): string => `\x1b[${String(n)}A`;
const ERASE_BELOW = "\x1b[0J";

/** Semantic tone → SGR code. Nothing here picks a colour by hand. */
const TONE_CODES: Record<string, string> = {
  muted: "2",
  info: "36",
  success: "32",
  warn: "33",
  error: "31",
};

/**
 * Two lines of headroom: the shell's own prompt has to fit under the block when
 * the run ends, and a block exactly as tall as the screen scrolls itself off the
 * top the moment anything else is printed.
 */
function blockHeight(rows: number | undefined): number {
  return Math.max(3, (rows ?? FALLBACK_ROWS) - 2);
}

export interface PinnedPane {
  /** The width a row must fit into, and the most rows the block may take. */
  width(): number;
  height(): number;
  /** One streamed line, printed above the block. */
  line(text: string): void;
  /** Replace what the block says and repaint it. */
  set(rows: PaneRow[]): void;
  /** Take the block down, leaving the transcript exactly as it was printed. */
  close(): void;
}

/** A pane that only prints. What a piped run gets. */
function plainPane(out: PaneOutput): PinnedPane {
  return {
    width(): number {
      return drawWidth(out.columns);
    },
    height(): number {
      return blockHeight(out.rows);
    },
    line(text: string): void {
      out.write(`${text}\n`);
    },
    set(): void {
      // Nothing is pinned, so there is nothing to repaint.
    },
    close(): void {
      // and nothing to take down.
    },
  };
}

/**
 * Open a pinned block over `out`, or a plain printer when there is no terminal
 * to pin it to.
 */
export function openPinnedPane(out: PaneOutput, isTTY: boolean): PinnedPane {
  if (!isTTY) return plainPane(out);

  let content: PaneRow[] = [];
  /** What is actually on screen right now — the cursor arithmetic's only input. */
  let drawn = 0;

  const paint = (row: PaneRow): string => {
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

  return {
    width(): number {
      return drawWidth(out.columns);
    },
    height(): number {
      return blockHeight(out.rows);
    },
    line(text: string): void {
      clear();
      out.write(`${text}\n`);
      draw();
    },
    set(rows: PaneRow[]): void {
      content = rows;
      clear();
      draw();
    },
    close(): void {
      clear();
      content = [];
    },
  };
}
