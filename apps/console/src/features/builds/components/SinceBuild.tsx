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

import { useState } from "react";
import { Box, Button, Typography } from "@wso2/oxygen-ui";
import type { LineBlock } from "../../spec/model/ids";
import { useBuilds } from "../api/builds";
import { builtLines, changeWords, featureChanges, lastBuildOf, type FeatureChanges } from "../model/changes";

/**
 * What changed in a feature since the build that last carried it, under its
 * page: a line of counts, opening to each change — an edited line with the
 * words it was built with, an added line, a retired one (which is no longer on
 * the page to point at). Nothing before the feature's first build.
 */
export function SinceBuild({ projectName, featureId, lines }: { projectName: string; featureId: string; lines: LineBlock[] }) {
  const builds = useBuilds(projectName).data;
  const [open, setOpen] = useState(false);
  const built = builds ? lastBuildOf(builds, featureId) : null;
  if (!built) return null;
  const changes = featureChanges(built.lines, builtLines(lines));
  const words = changeWords({ added: changes.added.length, edited: changes.edited.length, retired: changes.retired.length });
  return (
    <Box sx={{ mt: 2.75, maxWidth: "72ch" }}>
      <Typography variant="body2" color="text.secondary" component="div" sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        {words ? `Changed since ${built.version}: ${words}.` : `Built in ${built.version}, unchanged since.`}
        {words && (
          <Button size="small" variant="text" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
            {open ? "Hide" : "Show"}
          </Button>
        )}
      </Typography>
      {open && <ChangeList changes={changes} version={built.version} />}
    </Box>
  );
}

function ChangeList({ changes, version }: { changes: FeatureChanges; version: string }) {
  const rows = [
    ...changes.edited.map((e) => ({ key: `e:${e.id}`, label: "Edited", text: e.now, was: e.was })),
    ...changes.added.map((l, i) => ({ key: `a:${i}`, label: "Added", text: l.words, was: null })),
    ...changes.retired.map((l, i) => ({ key: `r:${i}`, label: "Retired", text: l.words, was: null })),
  ];
  return (
    <Box component="ul" sx={{ m: 0, mt: 1, pl: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 1 }}>
      {rows.map((r) => (
        <Box component="li" key={r.key} sx={{ borderLeft: "2px solid", borderColor: "divider", pl: 1.25 }}>
          <Typography variant="caption" color="text.secondary">
            {r.label}
          </Typography>
          <Typography
            variant="body2"
            sx={r.label === "Retired" ? { textDecoration: "line-through", color: "text.secondary" } : {}}
          >
            {r.text}
          </Typography>
          {r.was && (
            <Typography variant="body2" color="text.secondary">
              Built in {version} as: {r.was}
            </Typography>
          )}
        </Box>
      ))}
    </Box>
  );
}
