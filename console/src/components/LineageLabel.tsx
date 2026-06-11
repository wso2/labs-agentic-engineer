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

import { Chip, Stack, Typography } from '@wso2/oxygen-ui';

interface LineageLabelProps {
  sourceSpec?: string;
  sourceDesign?: string;
}

export default function LineageLabel({ sourceSpec, sourceDesign }: LineageLabelProps) {
  if (!sourceSpec && !sourceDesign) return null;

  const parts: string[] = [];
  if (sourceSpec) parts.push(sourceSpec);
  if (sourceDesign) parts.push(sourceDesign);

  return (
    <Chip
      label={
        <Stack direction="row" alignItems="center" gap={0.5}>
          <Typography variant="caption" sx={{ opacity: 0.7 }}>
            from
          </Typography>
          <Typography variant="caption" fontWeight={600}>
            {parts.join(', ')}
          </Typography>
        </Stack>
      }
      size="small"
      variant="outlined"
      sx={{ height: 24, borderStyle: 'dashed' }}
    />
  );
}
