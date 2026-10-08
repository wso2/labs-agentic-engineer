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
import { Alert, Box, Typography } from "@wso2/oxygen-ui";
import { renderMermaid } from "./mermaid";

// A design document as the design skill writes it — a flow, the domain
// model: a heading, a little prose, one mermaid diagram, and a few bullets.
// Drawn as written; the blocks the skill uses are the only ones it reads.

type Block =
  | { kind: "heading"; level: number; text: string }
  | { kind: "paragraph"; text: string }
  | { kind: "list"; items: string[] }
  | { kind: "diagram"; source: string };

function blocksOf(markdown: string): Block[] {
  const out: Block[] = [];
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  let i = 0;
  while (i < lines.length) {
    const line = lines[i]!;
    const trimmed = line.trim();
    if (trimmed.startsWith("```")) {
      const lang = trimmed.slice(3).trim();
      const body: string[] = [];
      i += 1;
      while (i < lines.length && !lines[i]!.trim().startsWith("```")) body.push(lines[i++]!);
      i += 1;
      out.push(lang === "mermaid" ? { kind: "diagram", source: body.join("\n") } : { kind: "paragraph", text: body.join("\n") });
      continue;
    }
    const heading = /^(#{1,6})\s+(.+)$/.exec(trimmed);
    if (heading) {
      out.push({ kind: "heading", level: heading[1]!.length, text: heading[2]! });
      i += 1;
      continue;
    }
    if (/^[-*]\s+/.test(trimmed)) {
      const items: string[] = [];
      while (i < lines.length && lines[i]!.trim() !== "" && !/^#{1,6}\s/.test(lines[i]!.trim()) && !lines[i]!.trim().startsWith("```")) {
        const l = lines[i]!.trim();
        if (/^[-*]\s+/.test(l)) items.push(l.replace(/^[-*]\s+/, ""));
        else if (items.length) items[items.length - 1] += ` ${l}`;
        i += 1;
      }
      out.push({ kind: "list", items });
      continue;
    }
    if (trimmed === "") {
      i += 1;
      continue;
    }
    const para: string[] = [];
    while (i < lines.length && lines[i]!.trim() !== "" && !/^(#{1,6}\s|[-*]\s|```)/.test(lines[i]!.trim())) para.push(lines[i++]!.trim());
    out.push({ kind: "paragraph", text: para.join(" ") });
  }
  return out;
}

/** `code` spans as code; the rest as text. */
function inline(text: string): ReactNode[] {
  return text.split(/(`[^`]+`)/).map((part, i) =>
    part.startsWith("`") && part.endsWith("`") ? (
      <Box key={i} component="code" sx={{ fontFamily: "monospace", fontSize: "0.85em" }}>
        {part.slice(1, -1)}
      </Box>
    ) : (
      part
    ),
  );
}

function Diagram({ source }: { source: string }) {
  const [svg, setSvg] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    renderMermaid(source).then(
      (out) => live && setSvg(out),
      () => live && setFailed(true),
    );
    return () => {
      live = false;
    };
  }, [source]);
  if (failed) return <Alert severity="warning">This diagram could not be drawn.</Alert>;
  if (!svg) return <Typography variant="caption" color="text.secondary">Drawing the diagram…</Typography>;
  // Mermaid renders with securityLevel strict: no scripts, no foreign HTML.
  return <Box sx={{ "& svg": { maxWidth: "100%", height: "auto" } }} dangerouslySetInnerHTML={{ __html: svg }} />;
}

export function DocumentView({ markdown }: { markdown: string }) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5, maxWidth: "80ch" }}>
      {blocksOf(markdown).map((b, i) => {
        switch (b.kind) {
          case "heading":
            return (
              <Typography key={i} variant={b.level === 1 ? "h6" : "subtitle1"} component={`h${Math.min(b.level + 1, 6)}` as "h2"}>
                {inline(b.text)}
              </Typography>
            );
          case "paragraph":
            return (
              <Typography key={i} variant="body2">
                {inline(b.text)}
              </Typography>
            );
          case "list":
            return (
              <Box key={i} component="ul" sx={{ m: 0, pl: 2.5 }}>
                {b.items.map((item, j) => (
                  <Typography key={j} component="li" variant="body2">
                    {inline(item)}
                  </Typography>
                ))}
              </Box>
            );
          case "diagram":
            return <Diagram key={i} source={b.source} />;
        }
      })}
    </Box>
  );
}
