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

import { useEffect, useState, type ReactNode } from "react";
import { Box, Typography } from "@wso2/oxygen-ui";
import type { LineBlock, Span } from "../../../spec/model/ids";
import { pillSx, soft } from "../../../spec/components/Tag";
import { decisionTag, keptText } from "../../model/decisions";

// The technical decisions, read-only as written, with Keep and Change… on a
// decision still tagged assumed (ADR-0006). Keep drops the tag in the room;
// the line then shows "by you" for a few seconds, as in the spec editor.
// Change… opens the file's 💬 box on the decision, quoted read-only above it.

/** How long a kept line shows "by you". */
const BY_YOU_MS = 4000;

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

/** The line's words, its italic runs in italics and its tag as a quiet "assumed". */
function words(line: LineBlock, tag: Span | null): ReactNode[] {
  const out: ReactNode[] = [];
  let at = 0;
  for (const e of line.emphasis) {
    if (e.start < at) continue;
    out.push(line.text.slice(at, e.start));
    const text = line.text.slice(e.start, e.end);
    out.push(
      tag && e.start === tag.start ? (
        <Box key={e.start} component="span" sx={{ fontSize: "0.75rem", color: "warning.main" }}>
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

export function DecisionsView({
  lines,
  ready,
  onKeep,
  onChange,
}: {
  lines: readonly LineBlock[];
  /** Whether the room is there to write to. */
  ready: boolean;
  /** Keep a decision by its line's text; false when nothing was written. */
  onKeep: (text: string) => boolean;
  /** Open the file's 💬 box on this decision, as it reads without its tag. */
  onChange: (decision: string) => void;
}) {
  const [byYou, setByYou] = useState<string | null>(null);
  useEffect(() => {
    if (byYou === null) return;
    const timer = window.setTimeout(() => setByYou(null), BY_YOU_MS);
    return () => window.clearTimeout(timer);
  }, [byYou]);

  const entry = (line: LineBlock, i: number) => {
    const tag = decisionTag(line);
    const kept = byYou !== null && tag === null && line.text === byYou;
    return (
      <Typography
        key={i}
        component="li"
        variant="body2"
        sx={{
          ml: (line.depth ?? 1) > 1 ? 2.5 : 0,
          borderRadius: 1.5,
          ...(tag ? { bgcolor: soft("warning", 0.14) } : {}),
          ...(kept ? { textDecoration: "underline dotted var(--oxygen-palette-success-main)", textUnderlineOffset: "4px" } : {}),
        }}
      >
        {words(line, tag)}
        {tag && (
          <Box component="span" sx={{ display: "inline-flex", flexWrap: "wrap", gap: 0.5, ml: 0.75, verticalAlign: "baseline" }}>
            <Box
              component="button"
              type="button"
              disabled={!ready}
              aria-label={`Keep: ${keptText(line)}`}
              onClick={() => {
                if (onKeep(line.text)) setByYou(keptText(line));
              }}
              sx={pillSx("warning")}
            >
              Keep
            </Box>
            <Box component="button" type="button" disabled={!ready} aria-label={`Change: ${keptText(line)}`} onClick={() => onChange(keptText(line).trim())} sx={pillSx("warning")}>
              Change…
            </Box>
          </Box>
        )}
        {kept && (
          <Box component="span" sx={{ fontSize: "0.6875rem", color: "success.main", whiteSpace: "nowrap", ml: 1, display: "inline-block" }}>
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
            {words(line, null)}
          </Typography>
        );
      })}
    </Box>
  );
}
