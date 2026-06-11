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

import { useEffect, useRef, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Typography,
} from '@wso2/oxygen-ui';
import { Package } from '@wso2/oxygen-ui-icons-react';
import {
  orgSkillsApi,
  SkillApiError,
  type SkillImportResult,
  type SkillValidationIssue,
} from '../../services/api/orgSkills';

interface SkillImportDialogProps {
  orgHandle: string;
  open: boolean;
  onClose: () => void;
  onImported: (name: string) => void;
}

/**
 * SkillImportDialog — upload an AgentSkills tarball (.tar.gz) to create a
 * kind=imported skill. Surfaces the server's license / compatibility /
 * warnings, and structured validation issues on rejection. Nothing
 * persists on a validation failure.
 */
export default function SkillImportDialog({ orgHandle, open, onClose, onImported }: SkillImportDialogProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [issues, setIssues] = useState<SkillValidationIssue[]>([]);
  const [result, setResult] = useState<SkillImportResult | null>(null);

  useEffect(() => {
    if (open) {
      setFile(null);
      setError(null);
      setIssues([]);
      setResult(null);
      // Reset the hidden input so re-selecting the same filename after a
      // reopen still fires onChange.
      if (inputRef.current) inputRef.current.value = '';
    }
  }, [open]);

  const handleUpload = async () => {
    if (!file) return;
    setUploading(true);
    setError(null);
    setIssues([]);
    try {
      const res = await orgSkillsApi.importTarball(orgHandle, file);
      setResult(res);
    } catch (e) {
      if (e instanceof SkillApiError && e.issues && e.issues.length > 0) {
        setIssues(e.issues);
      } else {
        setError(e instanceof Error ? e.message : String(e));
      }
    } finally {
      setUploading(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={uploading ? undefined : onClose}
      maxWidth="sm"
      fullWidth
      slotProps={{ paper: { sx: { backgroundColor: 'background.default', backgroundImage: 'none', opacity: 1, backdropFilter: 'none' } } }}
    >
      <DialogTitle>Import AgentSkills directory</DialogTitle>
      <DialogContent dividers>
        <Stack gap={2}>
          <Alert severity="info">
            Upload an AgentSkills tarball (.tar.gz) containing a single top-level
            directory with a SKILL.md and optional references/. Scripts and assets
            are rejected.
          </Alert>

          {!result && (
            <>
              <input
                ref={inputRef}
                type="file"
                accept=".tar.gz,.tgz,application/gzip"
                style={{ display: 'none' }}
                onChange={(e) => {
                  setFile(e.target.files?.[0] ?? null);
                  setError(null);
                  setIssues([]);
                }}
              />
              <Stack direction="row" gap={1.5} alignItems="center">
                <Button
                  variant="outlined"
                  startIcon={<Package size={18} />}
                  onClick={() => inputRef.current?.click()}
                  disabled={uploading}
                >
                  Choose file
                </Button>
                {file && (
                  <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                    {file.name}
                  </Typography>
                )}
              </Stack>
            </>
          )}

          {issues.length > 0 && (
            <Alert severity="error">
              <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
                Import rejected
              </Typography>
              <Stack gap={0.5}>
                {issues.map((i, idx) => (
                  <Typography key={idx} variant="caption">
                    <strong>{i.code}</strong>: {i.message}
                  </Typography>
                ))}
              </Stack>
            </Alert>
          )}
          {error && <Alert severity="error">{error}</Alert>}

          {result && (
            <Alert severity="success">
              <Typography variant="subtitle2">Imported {result.name}</Typography>
              {result.license && (
                <Typography variant="caption" display="block">
                  License: {result.license}
                </Typography>
              )}
              {result.compatibility && (
                <Typography variant="caption" display="block">
                  Compatibility: {result.compatibility}
                </Typography>
              )}
              {result.warnings.length > 0 && (
                <Box sx={{ mt: 1 }}>
                  <Typography variant="caption" sx={{ fontWeight: 600 }}>
                    Warnings
                  </Typography>
                  {result.warnings.map((w, idx) => (
                    <Typography key={idx} variant="caption" display="block">
                      • {w}
                    </Typography>
                  ))}
                </Box>
              )}
            </Alert>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        {result ? (
          <Button
            variant="contained"
            onClick={() => {
              onImported(result.name);
              onClose();
            }}
          >
            Done
          </Button>
        ) : (
          <>
            <Button onClick={onClose} disabled={uploading}>
              Cancel
            </Button>
            <Button
              variant="contained"
              onClick={handleUpload}
              disabled={!file || uploading}
              startIcon={uploading ? <CircularProgress size={16} /> : undefined}
            >
              {uploading ? 'Uploading…' : 'Import'}
            </Button>
          </>
        )}
      </DialogActions>
    </Dialog>
  );
}
