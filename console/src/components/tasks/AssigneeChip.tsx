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

import { alpha, Chip } from '@wso2/oxygen-ui';
import { Bot, User } from '@wso2/oxygen-ui-icons-react';

interface AssigneeChipProps {
  assignee?: string;
}

export function AssigneeChip({ assignee }: AssigneeChipProps) {
  const sx = {
    height: 20,
    fontSize: '0.65rem',
    fontWeight: 600,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    bgcolor: (t: any) => alpha(t.palette.primary.main, 0.13),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    borderColor: (t: any) => alpha(t.palette.primary.main, 0.33),
    color: 'primary.main',
    border: '1px solid',
    '& .MuiChip-label': { px: 0.75 },
    '& .MuiChip-icon': { ml: 0.5, color: 'inherit' },
  };

  return assignee ? (
    <Chip icon={<User size={10} />} label={assignee} size="small" sx={sx} />
  ) : (
    <Chip icon={<Bot size={10} />} label="Automated" size="small" sx={sx} />
  );
}
