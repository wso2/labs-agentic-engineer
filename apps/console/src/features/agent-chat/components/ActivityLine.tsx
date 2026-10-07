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

import { Box, CircularProgress, Typography } from "@wso2/oxygen-ui";
import { CircleAlert, FilePen } from "@wso2/oxygen-ui-icons-react";
import type { SpecFeature } from "../../spec/api/specModel";
import { namedFiles } from "../../spec/model/files";
import type { ActivityItem } from "../chatLog";

/** A written file as the reader knows it: "F4 Spending reports", "Product", or its file name. */
function fileTitle(path: string, features: Pick<SpecFeature, "id" | "name" | "path">[]): string {
  const feature = features.find((f) => f.path === path);
  if (feature) return `${feature.id} ${feature.name}`;
  return namedFiles(features, [path])[0]?.label ?? path.split("/").at(-1) ?? path;
}

const VERB: Record<ActivityItem["state"], Record<string, string>> = {
  writing: { add: "Writing", edit: "Writing", remove: "Removing" },
  done: { add: "Wrote", edit: "Updated", remove: "Removed" },
  failed: { add: "Couldn't write", edit: "Couldn't update", remove: "Couldn't remove" },
};

/** One line for a file the agent is writing or wrote, quieter than what it says. */
export function ActivityLine({
  item,
  features,
}: {
  item: ActivityItem;
  features: Pick<SpecFeature, "id" | "name" | "path">[];
}) {
  const failed = item.state === "failed";
  return (
    <Box
      role="status"
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 0.75,
        pl: 4,
        color: failed ? "error.main" : "text.secondary",
      }}
    >
      {item.state === "writing" ? (
        <CircularProgress size={11} color="inherit" aria-hidden />
      ) : failed ? (
        <CircleAlert size={13} aria-hidden />
      ) : (
        <FilePen size={13} aria-hidden />
      )}
      <Typography variant="caption" noWrap title={item.errorText}>
        {VERB[item.state][item.op] ?? VERB[item.state].edit} {fileTitle(item.path, features)}
      </Typography>
    </Box>
  );
}
