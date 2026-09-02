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
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Typography,
} from "@wso2/oxygen-ui";
import { CircleCheck, Upload } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import { useImportRequirements } from "../api/queries";

type RequirementsImportResult =
  components["schemas"]["RequirementsImportResult"];

/**
 * Upload a modernize-extract requirements bundle into an empty project.
 * Create-only on the server — the entry point in SpecFileList is only shown
 * when the project has no requirements files yet.
 */
export function ImportRequirementsDialog({
  open,
  onClose,
  projectName,
  onImported,
}: {
  open: boolean;
  onClose: () => void;
  projectName: string;
  /** Called after a successful import so the collab room can reseed from git. */
  onImported?: () => void;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [result, setResult] = useState<RequirementsImportResult | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);

  const importBundle = useImportRequirements(projectName);
  const pending = importBundle.isPending;
  const submitError =
    fileError ??
    (importBundle.isError ? importBundle.error.message : null);

  const resetSubmission = () => {
    importBundle.reset();
    setFile(null);
    setFileError(null);
    setResult(null);
  };

  const close = () => {
    resetSubmission();
    onClose();
  };

  const submit = () => {
    if (!file) return;
    setFileError(null);
    if (!/\.(tgz|tar\.gz)$/i.test(file.name)) {
      setFileError("Upload a .tar.gz / .tgz requirements bundle");
      return;
    }
    importBundle.mutate(file, {
      onSuccess: (data) => {
        setResult(data);
        onImported?.();
      },
    });
  };

  const warnings = result?.warnings ?? [];
  const files = result?.files ?? [];

  return (
    <Dialog
      open={open}
      onClose={close}
      maxWidth="sm"
      fullWidth
      data-testid="import-requirements-dialog"
    >
      <DialogTitle>Import requirements</DialogTitle>

      {result ? (
        <>
          <DialogContent
            sx={{ display: "flex", flexDirection: "column", gap: 2 }}
          >
            <Alert severity="success" icon={<CircleCheck size={20} />}>
              Requirements landed as <strong>{result.tag}</strong>. Run{" "}
              <strong>Generate design</strong> next.
            </Alert>
            <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
              <Chip
                label={`version ${result.version}`}
                size="small"
                variant="outlined"
              />
              {files.map((path) => (
                <Chip
                  key={path}
                  label={path.split("/").at(-1) ?? path}
                  size="small"
                  variant="outlined"
                />
              ))}
            </Box>
            {warnings.length > 0 && (
              <Alert severity="warning">
                <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
                  Imported with warnings
                </Typography>
                <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
                  {warnings.map((warning) => (
                    <li key={warning}>
                      <Typography variant="body2">{warning}</Typography>
                    </li>
                  ))}
                </Box>
              </Alert>
            )}
          </DialogContent>
          <DialogActions>
            <Button variant="contained" onClick={close}>
              Done
            </Button>
          </DialogActions>
        </>
      ) : (
        <>
          <DialogContent
            sx={{ display: "flex", flexDirection: "column", gap: 2 }}
          >
            <DialogContentText>
              Upload the <code>.tar.gz</code> produced by{" "}
              <code>/modernize-extract</code> in a legacy application repo.
              Only empty projects accept an import — the PRD must include
              numbered user stories.
            </DialogContentText>
            <Box>
              <Button
                component="label"
                variant="outlined"
                startIcon={<Upload size={18} />}
              >
                Choose file
                <input
                  type="file"
                  accept=".tgz,.tar.gz,application/gzip"
                  hidden
                  aria-label="Choose file"
                  onChange={(e) => {
                    setFileError(null);
                    setFile(e.target.files?.[0] ?? null);
                  }}
                />
              </Button>
              {file && (
                <Typography
                  variant="body2"
                  color="text.secondary"
                  sx={{ mt: 1 }}
                >
                  {file.name}
                </Typography>
              )}
            </Box>
            {submitError && <Alert severity="error">{submitError}</Alert>}
          </DialogContent>
          <DialogActions>
            <Button onClick={close}>Cancel</Button>
            <Button
              variant="contained"
              onClick={submit}
              disabled={!file || pending}
            >
              {pending ? "Importing…" : "Import"}
            </Button>
          </DialogActions>
        </>
      )}
    </Dialog>
  );
}
