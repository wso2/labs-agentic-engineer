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

import { Box, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@wso2/oxygen-ui";
import type { SourceDocument } from "../api/specModel";
import type { IdIndex } from "../model/ids";
import type { SpecTarget } from "../useSpecWorkspace";
import { DocKicker } from "./DocKicker";
import { QuietIdText } from "./QuietId";

/** A document the user attached: read once, cited by page. What it says, and where each point landed. */
export function SourceDocumentView({
  document,
  index,
  onOpen,
}: {
  document: SourceDocument;
  index: IdIndex;
  onOpen: (target: SpecTarget) => void;
}) {
  return (
    <Box sx={{ maxWidth: "72ch" }}>
      <DocKicker>
        <span>
          Document{document.pages > 0 ? ` · ${document.pages} pages` : ""} · read once, cited by place
        </span>
      </DocKicker>
      <Typography component="h1" sx={{ fontSize: "1.5rem", fontWeight: 600, mt: 0.25, mb: 1.25 }}>
        {document.title}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        What the agent took from it, and where each point landed. A point from it is the user's own word, cited by
        page; nothing from it is marked assumed.
        {document.rows.length === 0 && " The agent writes this when it reads the document at the kickoff."}
      </Typography>
      <Box sx={{ overflowX: "auto" }}>
        <Table size="small" aria-label="What the document says">
          <TableHead>
            <TableRow>
              <TableCell>Page</TableCell>
              <TableCell>It says</TableCell>
              <TableCell>Landed in</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {document.rows.map((row, i) => (
              <TableRow key={`${row.page}-${i}`}>
                <TableCell sx={{ fontFamily: "monospace", whiteSpace: "nowrap", verticalAlign: "top" }}>{row.page}</TableCell>
                <TableCell sx={{ verticalAlign: "top" }}>{row.says}</TableCell>
                <TableCell sx={{ verticalAlign: "top", color: row.landedIn ? "text.primary" : "text.secondary" }}>
                  {row.landedIn ? <QuietIdText text={row.landedIn} index={index} onOpen={onOpen} /> : "Not used"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Box>
    </Box>
  );
}
