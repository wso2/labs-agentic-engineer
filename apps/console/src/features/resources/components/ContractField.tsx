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

import { Box, Button, Chip, MenuItem, TextField, Typography } from "@wso2/oxygen-ui";
import { Upload } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import type { ContractDraft } from "../model/resourceDraft";
import { contractLabel, ContractLine } from "./ResourceSections";

// The contract document in a Registered External resource's draft: the one
// on file, and a new one to replace it, by an address the platform fetches
// or a file chosen here (after the old console's ContractFields). Neither
// given, the record keeps the document it has.

type ResourceContract = components["schemas"]["ResourceContract"];
type ResourceContractType = components["schemas"]["ResourceContractType"];

const TYPES: ResourceContractType[] = ["openapi", "graphql", "sdk", "asyncapi", "protobuf", "documentation"];

const ACCEPT: Record<ResourceContractType, string> = {
  openapi: ".yaml,.yml,.json",
  asyncapi: ".yaml,.yml,.json",
  sdk: ".json",
  graphql: ".graphql,.gql,.graphqls",
  protobuf: ".proto",
  documentation: ".md,.markdown,.html,.htm,.txt",
};

export function ContractField({
  current,
  value,
  onChange,
  error,
}: {
  current: ResourceContract | undefined;
  value: ContractDraft;
  onChange: (next: ContractDraft) => void;
  error?: string | undefined;
}) {
  const patch = (next: Partial<ContractDraft>) => onChange({ ...value, ...next });
  const picker = (
    <input
      type="file"
      accept={ACCEPT[value.type]}
      hidden
      onChange={(e) => {
        const file = e.target.files?.[0];
        e.target.value = "";
        if (!file) return;
        // A contract is text (YAML, JSON, SDL, proto) and goes on the wire as text.
        void file.text().then((content) => patch({ fileName: file.name, content, url: "" }));
      }}
    />
  );
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.25 }}>
      {current && <ContractLine contract={current} />}
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
        <TextField
          select
          size="small"
          label="Type"
          value={value.type}
          onChange={(e) => patch({ type: e.target.value as ResourceContractType })}
          sx={{ width: 160 }}
        >
          {TYPES.map((t) => (
            <MenuItem key={t} value={t}>
              {contractLabel(t)}
            </MenuItem>
          ))}
        </TextField>
        {value.fileName ? (
          <>
            <Chip label={value.fileName} color={error ? "error" : "default"} onDelete={() => patch({ fileName: "", content: "" })} />
            <Button component="label" size="small">
              Replace
              {picker}
            </Button>
          </>
        ) : (
          <>
            <TextField
              size="small"
              label={current ? "A new document's URL" : "The document's URL"}
              placeholder="https://"
              value={value.url}
              onChange={(e) => patch({ url: e.target.value })}
              sx={{ flex: 1, minWidth: 220 }}
            />
            <Button component="label" variant="outlined" size="small" startIcon={<Upload size={14} />}>
              Upload
              {picker}
            </Button>
          </>
        )}
      </Box>
      <Typography variant="caption" color={error ? "error" : "text.secondary"}>
        {error ??
          (current
            ? "Leave both empty to keep the document on file."
            : "Optional now: the platform fetches the URL, or keeps the file. Every project that reuses the resource gets a copy.")}
      </Typography>
    </Box>
  );
}
