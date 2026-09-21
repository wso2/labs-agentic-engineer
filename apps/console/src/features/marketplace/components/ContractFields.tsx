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
 * The resource's CONTRACT document, on the register form.
 *
 * A resource doc is reading material; the contract is the one document
 * consumers code against, and a project that reuses this resource gets a copy
 * of it beside its dependency. So it is its own block rather than another docs
 * row: one document, one type, and exactly one source — a URL the platform
 * fetches and hashes, or a file uploaded here.
 *
 * The block is optional. A resource may be registered before its document
 * exists, and an edit that touches neither field leaves the document already on
 * the record alone (the write carries no `contract` at all).
 */

import {
  Box,
  Button,
  Chip,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { Upload } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";

type ResourceContract = components["schemas"]["ResourceContract"];
type ResourceContractType = components["schemas"]["ResourceContractType"];
type ResourceContractWriteDTO = components["schemas"]["ResourceContractWriteDTO"];

export type ContractRow = {
  type: ResourceContractType;
  /** The address the platform fetches from; empty when a file was uploaded. */
  url: string;
  fileName: string;
  content: string;
  /** The document already on the record, shown as a fact on an edit. */
  path: string;
};

export function emptyContractRow(): ContractRow {
  return { type: "openapi", url: "", fileName: "", content: "", path: "" };
}

/** The block as an existing record's contract fills it. */
export function rowFromContract(contract: ResourceContract | undefined): ContractRow {
  if (!contract) return emptyContractRow();
  return { ...emptyContractRow(), type: contract.type, path: contract.path };
}

export const EMPTY_CONTRACT_FILE =
  "That file is empty. Choose the document itself, or remove it.";

/**
 * Why this block cannot be submitted, or undefined when it can. A chosen file
 * with no bytes is the one case: it reads as "no document" on the wire, so
 * submitting would quietly leave the record's document as it was.
 */
export function contractRowError(row: ContractRow): string | undefined {
  if (row.fileName.trim() && !row.content) return EMPTY_CONTRACT_FILE;
  return undefined;
}

/**
 * The write this block makes, or undefined when it makes none — the record's
 * document is then left as it is. An uploaded file wins over a URL: it is the
 * more recent thing the user did, and the two are exclusive on the wire.
 * Callers gate on `contractRowError` first; an empty file never reaches here.
 */
export function contractWriteFromRow(row: ContractRow): ResourceContractWriteDTO | undefined {
  const fileName = row.fileName.trim();
  if (fileName && row.content) {
    return { type: row.type, fileName, content: row.content };
  }
  const url = row.url.trim();
  if (url) return { type: row.type, url };
  return undefined;
}

const CONTRACT_TYPES: { value: ResourceContractType; label: string }[] = [
  { value: "openapi", label: "OpenAPI" },
  { value: "graphql", label: "GraphQL" },
  { value: "sdk", label: "SDK" },
  { value: "asyncapi", label: "AsyncAPI" },
  { value: "protobuf", label: "Protobuf" },
  { value: "documentation", label: "Documentation" },
];

function acceptFor(type: ResourceContractType): string {
  switch (type) {
    case "openapi":
    case "asyncapi":
    case "sdk":
      return ".yaml,.yml,.json,application/json,application/yaml,text/yaml";
    case "graphql":
      return ".graphql,.gql,.graphqls";
    case "protobuf":
      return ".proto,text/x-protobuf";
    case "documentation":
      return ".md,.markdown,.html,.htm,text/markdown,text/html,text/plain";
  }
}

export function ContractFields({
  contract,
  onChange,
}: {
  contract: ContractRow;
  onChange: (next: ContractRow) => void;
}) {
  const patch = (next: Partial<ContractRow>) => onChange({ ...contract, ...next });
  const picker = (
    <input
      type="file"
      accept={acceptFor(contract.type)}
      hidden
      onChange={(e) => {
        const file = e.target.files?.[0];
        e.target.value = "";
        if (!file) return;
        const reader = new FileReader();
        reader.onload = () => {
          // A contract document is text — YAML, JSON, GraphQL SDL, proto — and
          // the wire carries it as text, so it is read as UTF-8, never base64.
          patch({ fileName: file.name, content: String(reader.result ?? ""), url: "" });
        };
        reader.readAsText(file);
      }}
    />
  );

  return (
    <Box>
      <Typography variant="subtitle1" sx={{ mb: 0.5 }}>
        Contract document
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        Optional. The one document consumers code against. Give a URL for the platform to
        fetch, or upload the file.
      </Typography>
      {contractRowError(contract) ? (
        <Typography variant="caption" color="error" sx={{ display: "block", mb: 1 }}>
          {contractRowError(contract)}
        </Typography>
      ) : null}
      <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
        <TextField
          select
          size="small"
          label="Contract type"
          value={contract.type}
          onChange={(e) => patch({ type: e.target.value as ResourceContractType })}
          sx={{ width: 168, flexShrink: 0 }}
        >
          {CONTRACT_TYPES.map((t) => (
            <MenuItem key={t.value} value={t.value}>
              {t.label}
            </MenuItem>
          ))}
        </TextField>
        {contract.fileName ? (
          <>
            <Chip
              label={contract.fileName}
              color={contractRowError(contract) ? "error" : "default"}
              onDelete={() => patch({ fileName: "", content: "" })}
              sx={{ maxWidth: "100%", "& .MuiChip-label": { overflow: "hidden" } }}
            />
            <Button component="label" size="small">
              Replace
              {picker}
            </Button>
          </>
        ) : (
          <>
            <TextField
              size="small"
              label="Contract document URL"
              value={contract.url}
              onChange={(e) => patch({ url: e.target.value })}
              placeholder="https://"
              sx={{ flex: 1, minWidth: 0 }}
            />
            <Button
              component="label"
              variant="outlined"
              size="small"
              color="inherit"
              startIcon={<Upload size={14} />}
              sx={{ flexShrink: 0, borderRadius: 1 }}
            >
              Upload file
              {picker}
            </Button>
          </>
        )}
      </Stack>
      {contract.path && !contract.fileName && !contract.url.trim() && (
        <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1 }}>
          Current document: {contract.path}
        </Typography>
      )}
    </Box>
  );
}
