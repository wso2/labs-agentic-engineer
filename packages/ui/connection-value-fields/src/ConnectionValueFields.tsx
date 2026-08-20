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

import { Box, TextField } from "@wso2/oxygen-ui";

export interface ConnectionValueFieldConfig {
  key: string;
  description?: string;
  secret?: boolean;
}

export function ConnectionValueFields({
  config,
  values,
  onValueChange,
}: {
  config: readonly ConnectionValueFieldConfig[];
  values: Record<string, string>;
  onValueChange: (key: string, value: string) => void;
}) {
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
        gap: 1.25,
      }}
    >
      {config.map((key) => (
        <TextField
          key={key.key}
          label={key.key}
          {...(key.description && { helperText: key.description })}
          size="small"
          fullWidth
          {...(key.secret && { type: "password" })}
          value={values[key.key] ?? ""}
          onChange={(event) => onValueChange(key.key, event.target.value)}
          sx={{ "& input": { fontFamily: "monospace" } }}
        />
      ))}
    </Box>
  );
}
