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

import type React from 'react';
import { IconButton, Tooltip } from '@wso2/oxygen-ui';

export interface ToolbarButtonProps {
  label: string;
  icon: React.ReactNode;
  isActive?: boolean;
  onClick: () => void;
  disabled?: boolean;
}

export function ToolbarButton({
  label,
  icon,
  isActive = false,
  onClick,
  disabled = false,
}: ToolbarButtonProps) {
  const button = (
    <IconButton
      size="small"
      aria-label={label}
      aria-pressed={isActive}
      disabled={disabled}
      onClick={onClick}
      sx={{
        width: 30,
        height: 30,
        borderRadius: 1,
        color: isActive ? 'primary.main' : 'text.secondary',
        bgcolor: isActive ? 'color-mix(in srgb, currentColor 12%, transparent)' : 'transparent',
        '&:hover': {
          bgcolor: isActive
            ? 'color-mix(in srgb, currentColor 18%, transparent)'
            : 'action.hover',
        },
      }}
    >
      {icon}
    </IconButton>
  );

  if (disabled) return button;
  return <Tooltip title={label}>{button}</Tooltip>;
}
