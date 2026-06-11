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
import type { Editor } from '@tiptap/react';
import { Box, Button, Popover, Stack, TextField } from '@wso2/oxygen-ui';

export interface LinkPopoverProps {
  editor: Editor;
  anchorEl: HTMLElement | null;
  open: boolean;
  onClose: () => void;
}

export function LinkPopover({ editor, anchorEl, open, onClose }: LinkPopoverProps) {
  const existingUrl: string = editor.getAttributes('link').href ?? '';
  const [url, setUrl] = useState(existingUrl);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setUrl(existingUrl);
      // Focus after Popover mounts its content.
      requestAnimationFrame(() => {
        inputRef.current?.focus();
        inputRef.current?.select();
      });
    }
  }, [open, existingUrl]);

  const applyLink = () => {
    if (url.trim()) {
      editor.chain().focus().extendMarkRange('link').setLink({ href: url.trim() }).run();
    } else {
      editor.chain().focus().extendMarkRange('link').unsetLink().run();
    }
    onClose();
  };

  const removeLink = () => {
    editor.chain().focus().extendMarkRange('link').unsetLink().run();
    onClose();
  };

  return (
    <Popover
      open={open}
      anchorEl={anchorEl}
      onClose={onClose}
      anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
      transformOrigin={{ vertical: 'top', horizontal: 'left' }}
    >
      <Box sx={{ p: 1.5 }} onMouseDown={(e) => e.preventDefault()}>
        <Stack direction="row" spacing={1} alignItems="center">
          <TextField
            inputRef={inputRef}
            type="url"
            placeholder="https://..."
            value={url}
            size="small"
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                applyLink();
              }
              if (e.key === 'Escape') {
                e.preventDefault();
                onClose();
              }
            }}
            sx={{ width: 240 }}
          />
          <Button variant="contained" size="small" onClick={applyLink}>
            Apply
          </Button>
          {existingUrl && (
            <Button variant="text" size="small" color="error" onClick={removeLink}>
              Remove
            </Button>
          )}
        </Stack>
      </Box>
    </Popover>
  );
}
