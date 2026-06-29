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

import type { JSX } from 'react';
import Drawer from '@mui/material/Drawer';
import { Box, Chip, Divider, IconButton, Stack, Typography } from '@wso2/oxygen-ui';
import { X } from '@wso2/oxygen-ui-icons-react';
import type { DepRef } from './DependenciesSection';

const DRAWER_WIDTH = 480;

interface DependencyDrawerProps {
  open: boolean;
  depRef: DepRef | null;
  orgHandle: string;
  projectId: string;
  onClose: () => void;
  onChanged: () => void;
}

export function DependencyDrawer({
  open,
  depRef,
  onClose,
}: DependencyDrawerProps): JSX.Element {
  const dep = depRef?.dependency ?? null;

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      PaperProps={{
        sx: { width: DRAWER_WIDTH, display: 'flex', flexDirection: 'column' },
      }}
    >
      {/* Header */}
      <Box
        sx={{
          px: 2.5,
          py: 2,
          display: 'flex',
          alignItems: 'flex-start',
          gap: 1.5,
          flexShrink: 0,
        }}
      >
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" gap={1} flexWrap="wrap">
            <Typography
              variant="h6"
              sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            >
              {dep?.name ?? '—'}
            </Typography>
            {dep && (
              <Chip label={dep.kind} size="small" variant="outlined" sx={{ flexShrink: 0 }} />
            )}
          </Stack>
          {dep?.status && (
            <Typography variant="caption" color="text.secondary">
              Status: {dep.status}
              {dep.reason ? ` · ${dep.reason}` : ''}
            </Typography>
          )}
        </Box>
        <IconButton
          aria-label="Close dependency drawer"
          size="small"
          onClick={onClose}
          sx={{ flexShrink: 0, mt: 0.5 }}
        >
          <X size={16} />
        </IconButton>
      </Box>

      <Divider />

      {/* Body — resolution UI filled in by tasks B2–B4 */}
      <Box sx={{ flex: 1, overflow: 'auto', px: 2.5, py: 2.5 }}>
        {dep ? (
          <Typography variant="body2" color="text.secondary">
            Resolution UI arrives in B2–B4.
          </Typography>
        ) : (
          <Typography variant="body2" color="text.secondary">
            Select a dependency to see details.
          </Typography>
        )}
      </Box>
    </Drawer>
  );
}
