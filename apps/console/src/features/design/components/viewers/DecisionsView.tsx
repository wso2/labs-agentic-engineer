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

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Box, Typography } from "@wso2/oxygen-ui";
import type { LineBlock, Span } from "../../../spec/model/ids";
import { assumedActionsSx, assumedLineSx, assumedTagSx, BY_YOU_MS, byYouLineSx, byYouSx, pillSx } from "../../../spec/components/assumedLook";
import { decisionTag, keptText } from "../../model/decisions";

// The technical decisions, read-only as written, with Keep and Change… on a
// decision still tagged assumed (ADR-0006), drawn as the spec editor draws an
// assumed line. Keep drops the tag in the room; each kept line then shows "by
// you" for a few seconds. Change… opens the file's 💬 box on the decision,
// quoted read-only above it.

type Group = { kind: "list"; lines: LineBlock[] } | { kind: "block"; line: LineBlock };

function groups(lines: readonly LineBlock[]): Group[] {
  const out: Group[] = [];
  for (const line of lines) {
    const last = out.at(-1);
    if (line.kind === "listItem" && last?.kind === "list") last.lines.push(line);
    else out.push(line.kind === "listItem" ? { kind: "list", lines: [line] } : { kind: "block", line });
  }
  return out;
}

/** A line's text drawn: its italic runs in italics, and its tag as a quiet "assumed". */
function lineContent(line: LineBlock, tag: Span | null): ReactNode[] {
  const out: ReactNode[] = [];
  let at = 0;
  for (const e of line.emphasis) {
    if (e.start < at) continue;
    out.push(line.text.slice(at, e.start));
    const text = line.text.slice(e.start, e.end);
    out.push(
      tag && e.start === tag.start ? (
        <Box key={e.start} component="span" sx={assumedTagSx}>
          {text}
        </Box>
      ) : (
        <em key={e.start}>{text}</em>
      ),
    );
    at = e.end;
  }
  out.push(line.text.slice(at));
  return out;
}

/** The lines the user kept here, by their text once kept, each for BY_YOU_MS. */
function useByYou(): { kept: ReadonlySet<string>; mark: (text: string) => void } {
  const [kept, setKept] = useState<ReadonlySet<string>>(new Set());
  const timers = useRef(new Set<number>());
  useEffect(() => {
    const live = timers.current;
    return () => live.forEach((t) => window.clearTimeout(t));
  }, []);
  const mark = (text: string) => {
    setKept((k) => new Set(k).add(text));
    const timer = window.setTimeout(() => {
      timers.current.delete(timer);
      setKept((k) => {
        const next = new Set(k);
        next.delete(text);
        return next;
      });
    }, BY_YOU_MS);
    timers.current.add(timer);
  };
  return { kept, mark };
}

export function DecisionsView({
  lines,
  ready,
  onKeep,
  onChangeDecision,
}: {
  lines: readonly LineBlock[];
  /** Whether the room is there to write to. */
  ready: boolean;
  /** Keep a decision by its line's text; false when nothing was written. */
  onKeep: (text: string) => boolean;
  /** Change… was pressed: open the file's 💬 box on this decision, as it reads without its tag. */
  onChangeDecision: (decision: string) => void;
}) {
  const byYou = useByYou();

  const entry = (line: LineBlock, i: number) => {
    const tag = decisionTag(line);
    const kept = tag === null && byYou.kept.has(line.text);
    return (
      <Typography
        key={i}
        component="li"
        variant="body2"
        sx={{ ml: (line.depth ?? 1) > 1 ? 2.5 : 0, borderRadius: 1.5, ...(tag ? assumedLineSx : {}), ...(kept ? byYouLineSx : {}) }}
      >
        {lineContent(line, tag)}
        {tag && (
          <Box component="span" sx={assumedActionsSx}>
            <Box
              component="button"
              type="button"
              disabled={!ready}
              aria-label={`Keep: ${keptText(line)}`}
              onClick={() => {
                if (onKeep(line.text)) byYou.mark(keptText(line));
              }}
              sx={pillSx("warning")}
            >
              Keep
            </Box>
            <Box
              component="button"
              type="button"
              disabled={!ready}
              aria-label={`Change: ${keptText(line)}`}
              onClick={() => onChangeDecision(keptText(line).trim())}
              sx={pillSx("warning")}
            >
              Change…
            </Box>
          </Box>
        )}
        {kept && (
          <Box component="span" sx={byYouSx}>
            by you
          </Box>
        )}
      </Typography>
    );
  };

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5, maxWidth: "80ch" }}>
      {groups(lines).map((g, i) => {
        if (g.kind === "list") {
          return (
            <Box key={i} component="ul" sx={{ m: 0, pl: 2.5, display: "flex", flexDirection: "column", gap: 0.5 }}>
              {g.lines.map(entry)}
            </Box>
          );
        }
        const { line } = g;
        return line.kind === "heading" ? (
          <Typography key={i} variant={line.level === 1 ? "h6" : "subtitle1"} component={`h${Math.min((line.level ?? 1) + 1, 6)}` as "h2"}>
            {line.text}
          </Typography>
        ) : (
          <Typography key={i} variant="body2">
            {lineContent(line, null)}
          </Typography>
        );
      })}
    </Box>
  );
}
