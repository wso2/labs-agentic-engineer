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
 * ProvideInterfaceDialog — the user's two routes to a dependency's interface
 * document: a URL the platform fetches, or a file dropped in. It lands in the
 * dependency's own directory beside the definition (ADR-0027), and the view
 * behind the dialog then links to it in place. A modal rather than a form in
 * the document: the document is what the dependency IS, and the upload is an
 * act on it.
 */

import { useRef, useState } from "react";
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { Upload } from "@wso2/oxygen-ui-icons-react";
import { useProvideDependencyContract } from "../api/queries";

export function ProvideInterfaceDialog({
  projectName,
  name,
  replacing,
  open,
  onClose,
  onCommitted,
}: {
  projectName: string;
  name: string;
  /** True when an interface is already on file (assumed or real) — the copy says "replace". */
  replacing: boolean;
  open: boolean;
  onClose: () => void;
  /** The document landed — the view behind refreshes what it shows. */
  onCommitted?: (() => void) | undefined;
}) {
  const provide = useProvideDependencyContract(projectName);
  const [url, setUrl] = useState("");
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement | null>(null);

  const close = () => {
    if (provide.isPending) return;
    setUrl("");
    provide.reset();
    onClose();
  };
  const submit = (body: { url: string } | { content: string }) =>
    provide.mutate(
      { depName: name, ...body },
      {
        onSuccess: () => {
          onCommitted?.();
          close();
        },
      },
    );
  const submitFile = (file: File | undefined) => {
    if (!file) return;
    void file.text().then((content) => submit({ content }));
  };

  return (
    <Dialog open={open} onClose={close} maxWidth="sm" fullWidth>
      <DialogTitle>
        <Typography variant="h6" fontWeight={700}>
          {name} · {replacing ? "Replace the interface" : "Provide the interface"}
        </Typography>
      </DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <Typography variant="body2" color="text.secondary">
            {replacing
              ? "A document from the provider replaces the interface on file. Paste the URL of its published OpenAPI document, or drop the file here."
              : "Paste the URL of the provider's published OpenAPI document, or drop the file here. The platform validates it and commits it beside this definition."}
          </Typography>
          <Stack direction="row" spacing={1}>
            <TextField
              size="small"
              fullWidth
              label="OpenAPI document URL"
              placeholder="https://…/openapi.yaml"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              disabled={provide.isPending}
            />
            <Button
              variant="outlined"
              disabled={url.trim() === "" || provide.isPending}
              loading={provide.isPending}
              onClick={() => submit({ url: url.trim() })}
            >
              Fetch
            </Button>
          </Stack>
          <Box
            role="button"
            tabIndex={0}
            aria-label="Drop the interface file here, or choose one"
            onClick={() => fileInput.current?.click()}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") fileInput.current?.click();
            }}
            onDragOver={(e) => {
              e.preventDefault();
              setDragging(true);
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragging(false);
              submitFile(e.dataTransfer.files[0]);
            }}
            sx={{
              border: 1,
              borderStyle: "dashed",
              borderColor: dragging ? "primary.main" : "divider",
              borderRadius: 1,
              p: 2,
              display: "flex",
              alignItems: "center",
              gap: 1.5,
              cursor: "pointer",
              color: "text.secondary",
            }}
          >
            <Upload size={18} />
            <Typography variant="body2">
              Drop an OpenAPI document (YAML or JSON) here, or click to choose one.
            </Typography>
            <input
              ref={fileInput}
              type="file"
              accept=".yaml,.yml,.json,application/json,text/yaml"
              hidden
              onChange={(e) => {
                submitFile(e.target.files?.[0]);
                e.target.value = "";
              }}
            />
          </Box>
          {provide.isError && (
            <Typography variant="body2" color="error">
              {provide.error instanceof Error ? provide.error.message : "The document was not accepted."}
            </Typography>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={provide.isPending}>
          Cancel
        </Button>
      </DialogActions>
    </Dialog>
  );
}
