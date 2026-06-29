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
 * ProvideSpec — the drawer body for an external dependency that has
 * `needsSpec: true` and no `specPath` yet. The user can either paste raw
 * OpenAPI YAML/JSON, upload a .yaml/.yml/.json file (its text is loaded into
 * the paste field), or supply a publicly reachable URL. Submitting calls the
 * A4 endpoint (collectSpec) and on success shows the returned operationCount,
 * then calls `onChanged()` so the parent can refresh the design query.
 */

import type { JSX } from 'react';
import { useRef, useState } from 'react';
import { Alert, Box, Button, Stack, TextField, Typography } from '@wso2/oxygen-ui';
import { useMutation } from '@tanstack/react-query';
import type { Dependency } from '../../services/api/types';
import { collectSpec } from '../../services/api/specs';
import { ApiError } from '../../services/api/rest';

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

interface ProvideSpecProps {
  orgHandle: string;
  projectId: string;
  /** The component that declares this dependency. */
  component: string;
  dep: Dependency;
  onChanged: () => void;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function ProvideSpec({
  orgHandle,
  projectId,
  component,
  dep,
  onChanged,
}: ProvideSpecProps): JSX.Element {
  const [pasteText, setPasteText] = useState('');
  const [specUrl, setSpecUrl] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);

  const mutation = useMutation({
    mutationFn: () => {
      const body: { rawSpec: string } | { specUrl: string } = pasteText.trim()
        ? { rawSpec: pasteText.trim() }
        : { specUrl: specUrl.trim() };
      return collectSpec(orgHandle, projectId, component, dep.name, body);
    },
    onSuccess: () => {
      onChanged();
    },
  });

  const canSubmit = pasteText.trim().length > 0 || specUrl.trim().length > 0;

  const errorMsg = mutation.isError
    ? mutation.error instanceof ApiError
      ? `${mutation.error.message} (HTTP ${mutation.error.status})`
      : mutation.error instanceof Error
        ? mutation.error.message
        : 'Failed to attach spec.'
    : null;

  // Handle file selection — read file text into the paste field.
  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (evt) => {
      const text = evt.target?.result;
      if (typeof text === 'string') {
        setPasteText(text);
      }
    };
    reader.readAsText(file);
    // Reset the input so the same file can be re-selected if needed.
    e.target.value = '';
  }

  if (mutation.isSuccess && mutation.data) {
    return (
      <Box>
        <Alert severity="success">
          Spec attached ✓ ({mutation.data.operationCount} operations)
        </Alert>
        <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
          {mutation.data.specPath}
        </Typography>
      </Box>
    );
  }

  return (
    <Box>
      {dep.description && (
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {dep.description}
        </Typography>
      )}

      <Stack spacing={2}>
        {/* Paste textarea */}
        <TextField
          label="Paste spec"
          placeholder="Paste OpenAPI YAML or JSON here…"
          multiline
          minRows={6}
          maxRows={16}
          value={pasteText}
          onChange={(e) => setPasteText(e.target.value)}
          fullWidth
          size="small"
          inputProps={{ 'aria-label': 'Paste spec' }}
        />

        {/* Hidden file input — triggered by "Upload file" button */}
        <input
          ref={fileInputRef}
          type="file"
          accept=".yaml,.yml,.json"
          style={{ display: 'none' }}
          onChange={handleFileChange}
          aria-label="Upload spec file"
        />
        <Button
          variant="outlined"
          size="small"
          onClick={() => fileInputRef.current?.click()}
          sx={{ alignSelf: 'flex-start' }}
        >
          Upload file (.yaml / .json)
        </Button>

        {/* URL field */}
        <TextField
          label="Spec URL"
          placeholder="https://example.com/openapi.yaml"
          value={specUrl}
          onChange={(e) => setSpecUrl(e.target.value)}
          fullWidth
          size="small"
          inputProps={{ 'aria-label': 'Spec URL' }}
        />

        {errorMsg && (
          <Alert severity="error">{errorMsg}</Alert>
        )}

        <Button
          variant="contained"
          onClick={() => mutation.mutate()}
          disabled={!canSubmit || mutation.isPending}
        >
          {mutation.isPending ? 'Attaching…' : 'Attach spec'}
        </Button>
      </Stack>
    </Box>
  );
}
