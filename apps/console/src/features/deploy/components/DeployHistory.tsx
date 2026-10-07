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

import { Box, Chip, Skeleton, Typography } from "@wso2/oxygen-ui";
import { stamp } from "../../../lib/stamp";
import type { HistoryRow } from "../model/history";

function when(at: NonNullable<HistoryRow["at"]>): string {
  return at.of === "build" ? `build finished ${stamp(at.iso)}` : stamp(at.iso);
}

/** What was deployed where, newest first; null while the version ledger loads. */
export function DeployHistory({ rows }: { rows: HistoryRow[] | null }) {
  return (
    <Box component="section" aria-labelledby="deploy-history-heading">
      <Typography id="deploy-history-heading" component="h2" sx={{ fontSize: "0.8125rem", fontWeight: 600, mb: 1 }}>
        History
      </Typography>
      {rows === null ? (
        <Skeleton width="50%" />
      ) : rows.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          Nothing has been deployed yet.
        </Typography>
      ) : (
        <Box component="ul" sx={{ listStyle: "none", m: 0, p: 0, display: "flex", flexDirection: "column", gap: 0.75 }}>
          {rows.map((row) => (
            <Box component="li" key={row.key} sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
              {row.version && (
                <Typography variant="body2" sx={{ fontWeight: 600, fontFamily: "monospace" }}>
                  {row.version}
                </Typography>
              )}
              <Typography variant="body2">
                in {row.environment}: {row.what}
                {row.at && (
                  <Typography component="span" variant="body2" color="text.secondary">
                    , {when(row.at)}
                  </Typography>
                )}
              </Typography>
              {row.current && <Chip size="small" variant="outlined" color="success" label="Running now" />}
            </Box>
          ))}
        </Box>
      )}
      <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1 }}>
        Promotions will be listed here once promoting is available.
      </Typography>
    </Box>
  );
}
