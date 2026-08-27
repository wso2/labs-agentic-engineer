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

import {
  Box,
  Button,
  Chip,
  IconButton,
  MenuItem,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from "@wso2/oxygen-ui";
import { Plus, Trash2, Upload } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";

type ResourceDocPointerDTO = components["schemas"]["ResourceDocPointerDTO"];
type ResourceDocWriteDTO = components["schemas"]["ResourceDocWriteDTO"];
type DocType = ResourceDocPointerDTO["type"];
type DocSource = "url" | "file";

export type ResourceDocRow = {
  type: DocType;
  source: DocSource;
  url: string;
  fileName: string;
  content: string;
  path: string;
};

function emptyDocRow(): ResourceDocRow {
  return {
    type: "documentation",
    source: "url",
    url: "",
    fileName: "",
    content: "",
    path: "",
  };
}

function fileNameFromPath(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash >= 0 ? path.slice(slash + 1) : path;
}

export function rowsFromPointers(
  docs: ResourceDocPointerDTO[],
): ResourceDocRow[] {
  return docs.map((d) => {
    if (d.path && !d.url) {
      return {
        type: d.type,
        source: "file",
        url: "",
        fileName: fileNameFromPath(d.path),
        content: "",
        path: d.path,
      };
    }
    return {
      type: d.type,
      source: "url",
      url: d.url ?? "",
      fileName: "",
      content: "",
      path: "",
    };
  });
}

export function writesFromRows(rows: ResourceDocRow[]): ResourceDocWriteDTO[] {
  const minted: ResourceDocWriteDTO[] = [];
  for (const row of rows) {
    if (row.source === "url") {
      const url = row.url.trim();
      if (!url) continue;
      minted.push({ type: row.type, url });
      continue;
    }
    if (row.content) {
      const fileName = row.fileName.trim();
      if (!fileName) continue;
      minted.push({ type: row.type, fileName, content: row.content });
      continue;
    }
    const path = row.path.trim();
    if (path) {
      minted.push({ type: row.type, path });
    }
  }
  return minted;
}

function acceptFor(type: DocType): string {
  switch (type) {
    case "openapi":
    case "asyncapi":
      return ".yaml,.yml,.json,application/json,application/yaml,text/yaml";
    case "graphql":
      return ".graphql,.gql";
    case "protobuf":
      return ".proto,text/x-protobuf";
    case "documentation":
      return ".md,.markdown,.html,.htm,text/markdown,text/html,text/plain";
  }
}

function patch(
  rows: ResourceDocRow[],
  index: number,
  next: Partial<ResourceDocRow>,
): ResourceDocRow[] {
  return rows.map((row, i) => (i === index ? { ...row, ...next } : row));
}

export function ResourceDocsFields({
  docs,
  onChange,
}: {
  docs: ResourceDocRow[];
  onChange: (docs: ResourceDocRow[]) => void;
}) {
  return (
    <Box>
      <Stack direction="row" sx={{ justifyContent: "space-between", mb: 0.5 }}>
        <Typography variant="subtitle1">Resource docs</Typography>
        <Button
          size="small"
          startIcon={<Plus size={14} />}
          onClick={() => onChange([...docs, emptyDocRow()])}
        >
          Add doc
        </Button>
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        Optional. Each doc is a URL or a file — pick one source per row.
      </Typography>
      {docs.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          No docs yet. Add a spec or documentation if agents should read one.
        </Typography>
      ) : (
        <Stack spacing={2}>
          {docs.map((doc, index) => (
            <DocRowFields
              key={index}
              doc={doc}
              onChange={(next) => onChange(patch(docs, index, next))}
              onRemove={() => onChange(docs.filter((_, i) => i !== index))}
            />
          ))}
        </Stack>
      )}
    </Box>
  );
}

const CONTROL_HEIGHT = 40;

function DocRowFields({
  doc,
  onChange,
  onRemove,
}: {
  doc: ResourceDocRow;
  onChange: (next: Partial<ResourceDocRow>) => void;
  onRemove: () => void;
}) {
  return (
    <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
      <TextField
        select
        size="small"
        label="Type"
        value={doc.type}
        onChange={(e) => onChange({ type: e.target.value as DocType })}
        sx={{ width: 168, flexShrink: 0 }}
      >
        <MenuItem value="documentation">Documentation</MenuItem>
        <MenuItem value="openapi">OpenAPI</MenuItem>
        <MenuItem value="graphql">GraphQL</MenuItem>
        <MenuItem value="asyncapi">AsyncAPI</MenuItem>
        <MenuItem value="protobuf">Protobuf</MenuItem>
      </TextField>
      <ToggleButtonGroup
        exclusive
        size="small"
        value={doc.source}
        onChange={(_, next: DocSource | null) => {
          if (!next || next === doc.source) return;
          if (next === "url") {
            onChange({
              source: "url",
              fileName: "",
              path: "",
              content: "",
            });
          } else {
            onChange({ source: "file", url: "" });
          }
        }}
        aria-label="Doc source"
        sx={{
          flexShrink: 0,
          "& .MuiToggleButton-root": {
            textTransform: "none",
            px: 1.5,
            height: CONTROL_HEIGHT,
          },
        }}
      >
        <ToggleButton value="url">URL</ToggleButton>
        <ToggleButton value="file">File</ToggleButton>
      </ToggleButtonGroup>
      {doc.source === "url" ? (
        <TextField
          size="small"
          label="URL"
          value={doc.url}
          onChange={(e) => onChange({ url: e.target.value })}
          placeholder="https://"
          sx={{ flex: 1, minWidth: 0 }}
        />
      ) : (
        <FileSlot
          type={doc.type}
          fileName={doc.fileName}
          onPick={(file) => {
            const reader = new FileReader();
            reader.onload = () => {
              onChange({
                fileName: file.name,
                content: String(reader.result ?? ""),
                path: "",
              });
            };
            reader.readAsText(file);
          }}
          onClear={() => onChange({ fileName: "", content: "", path: "" })}
        />
      )}
      <IconButton aria-label="Remove doc" onClick={onRemove}>
        <Trash2 size={16} />
      </IconButton>
    </Stack>
  );
}

function FileSlot({
  type,
  fileName,
  onPick,
  onClear,
}: {
  type: DocType;
  fileName: string;
  onPick: (file: File) => void;
  onClear: () => void;
}) {
  const picker = (
    <input
      type="file"
      accept={acceptFor(type)}
      hidden
      onChange={(e) => {
        const file = e.target.files?.[0];
        if (file) onPick(file);
        e.target.value = "";
      }}
    />
  );

  return (
    <Stack
      direction="row"
      spacing={1}
      sx={{ flex: 1, minWidth: 0, alignItems: "center" }}
    >
      {fileName ? (
        <>
          <Chip
            label={fileName}
            onDelete={onClear}
            sx={{ maxWidth: "100%", "& .MuiChip-label": { overflow: "hidden" } }}
          />
          <Button component="label" size="small">
            Replace
            {picker}
          </Button>
        </>
      ) : (
        <Button
          component="label"
          variant="outlined"
          size="small"
          color="inherit"
          startIcon={<Upload size={14} />}
          aria-label="Choose file"
          sx={{ height: CONTROL_HEIGHT, borderRadius: 1 }}
        >
          Choose file
          {picker}
        </Button>
      )}
    </Stack>
  );
}
