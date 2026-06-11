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

import { Accordion, AccordionDetails, AccordionSummary, Box, Typography, useTheme } from '@wso2/oxygen-ui';
import { ChevronDown } from '@wso2/oxygen-ui-icons-react';
import { TaskRow } from './TaskRow';
import type { Task } from '../../services/api';
import type { SectionConfig } from './types';

interface TaskSectionProps {
  section: SectionConfig;
  tasks: Task[];
  orgId: string;
  projectId: string;
  expanded: boolean;
  onExpandedChange: (expanded: boolean) => void;
}

export function TaskSection({ section, tasks, orgId, projectId, expanded, onExpandedChange }: TaskSectionProps) {
  const theme = useTheme();

  const labelColor = section.isPrimary ? theme.palette.primary.main : theme.palette.text.secondary;

  return (
    <Accordion
      expanded={expanded}
      onChange={(_, val) => onExpandedChange(val)}
      disableGutters
      elevation={0}
      sx={{
        mb: 0.75,
        border: '1px solid',
        borderColor: 'divider',
        borderRadius: 1.25,
        '&:before': { display: 'none' },
        overflow: 'hidden',
      }}
    >
      <AccordionSummary
        expandIcon={<ChevronDown size={14} style={{ color: labelColor }} />}
        sx={{
          minHeight: 40,
          px: 1.75,
          py: 0,
          '& .MuiAccordionSummary-content': { my: 1, alignItems: 'center', gap: 0.75 },
        }}
      >
        <Typography sx={{ fontSize: '0.72rem', fontWeight: 700, color: labelColor, textTransform: 'uppercase', letterSpacing: '0.06em' }}>
          {section.label}
        </Typography>
        <Typography sx={{ fontSize: '0.72rem', fontWeight: 500, color: labelColor, opacity: 0.7 }}>
          {tasks.length}
        </Typography>
      </AccordionSummary>

      <AccordionDetails sx={{ p: 1.25, display: 'flex', flexDirection: 'column', gap: 0.75 }}>
        {tasks.length > 0 ? (
          tasks.map((task, i) => (
            <TaskRow
              key={task.id}
              task={task}
              section={section}
              orgId={orgId}
              projectId={projectId}
              index={i}
            />
          ))
        ) : (
          <Box sx={{ py: 2, display: 'flex', justifyContent: 'center' }}>
            <Typography variant="body2" color="text.disabled">No tasks</Typography>
          </Box>
        )}
      </AccordionDetails>
    </Accordion>
  );
}
