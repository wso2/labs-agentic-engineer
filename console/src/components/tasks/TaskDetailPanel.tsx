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

import { useState } from 'react';
import {
  alpha,
  Box,
  Button,
  CircularProgress,
  Tooltip,
  Typography,
} from '@wso2/oxygen-ui';
import ReactMarkdown from 'react-markdown';
import { Play, RotateCcw, ExternalLink } from '@wso2/oxygen-ui-icons-react';
import { api } from '../../services/api';
import type { Task } from '../../services/api';
import { DependencyDrawer } from '../../pages/architecture/DependencyDrawer';
import type { DepRef } from '../../pages/architecture/DependenciesSection';
import { AssigneeChip } from './AssigneeChip';

interface TaskDetailPanelProps {
  task: Task;
  orgId: string;
  projectId: string;
  onClose: () => void;
}

export function TaskDetailPanel({ task, orgId, projectId, onClose }: TaskDetailPanelProps) {
  const [isExecuting, setIsExecuting] = useState(false);
  const [isRetrying, setIsRetrying] = useState(false);
  const [drawerDepRef, setDrawerDepRef] = useState<DepRef | null>(null);
  const [isOpeningDrawer, setIsOpeningDrawer] = useState(false);

  // resource-provisioning / config-collection tasks are resolved in the
  // dependency drawer (Provision / Provide configuration), NOT the per-task
  // exec endpoint (a no-op). The drawer opens as an overlay ON the tasks page —
  // no navigation to the architecture view.
  const drawerDep =
    task.type === 'resource-provisioning' ? task.resourceName
    : task.type === 'config-collection' ? task.connectionName
    : undefined;
  const drawerCtaLabel =
    task.type === 'resource-provisioning' ? 'Provision resource'
    : task.type === 'config-collection' ? 'Provide configuration'
    : '';
  const needsDrawerAction =
    !!drawerDep && (!task.status || ['pending', 'on_hold', 'failed'].includes(task.status));

  // Resolve the task's dep NAME → the full dependency object the drawer needs.
  // NB: a resource-provisioning / config-collection task's componentName is the
  // resource/connection name, NOT the owning component — so we search for the dep
  // by name across every component (the owner is whichever component declares it).
  const openDrawer = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!drawerDep) return;
    setIsOpeningDrawer(true);
    try {
      const design = await api.getDesign(orgId, projectId);
      for (const comp of design?.components ?? []) {
        const dep = comp.dependencies?.find((d) => d.name === drawerDep);
        if (dep) {
          setDrawerDepRef({ component: comp.name, dependency: dep });
          break;
        }
      }
    } catch {
      // swallow — the button stays; the user can retry
    } finally {
      setIsOpeningDrawer(false);
    }
  };

  // Re-read the design after a drawer action so the open drawer reflects the
  // fresh dependency (provisioning kicked off / values saved).
  const refreshDrawerDep = async () => {
    if (!drawerDep) return;
    const design = await api.getDesign(orgId, projectId);
    for (const comp of design?.components ?? []) {
      const dep = comp.dependencies?.find((d) => d.name === drawerDep);
      if (dep) {
        setDrawerDepRef({ component: comp.name, dependency: dep });
        break;
      }
    }
  };

  const handleExecute = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!task.componentTaskId) return;
    setIsExecuting(true);
    try {
      await api.execTask(orgId, projectId, task.componentTaskId);
    } catch {
      // silently fail — user can retry
    } finally {
      setIsExecuting(false);
      onClose();
    }
  };

  const handleRetry = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!task.componentTaskId) return;
    setIsRetrying(true);
    try {
      await api.retryTask(orgId, projectId, task.componentTaskId);
    } catch {
      // silently fail — board polling will refresh state
    } finally {
      setIsRetrying(false);
      onClose();
    }
  };

  return (
    <>
    <Box
      onClick={(e) => e.stopPropagation()}
      sx={{
        borderTop: '1px solid',
        borderColor: 'divider',
        px: 2,
        py: 1.75,
        display: 'flex',
        flexDirection: 'column',
        gap: 1.5,
        bgcolor: (t) => alpha(t.palette.text.primary, 0.015),
      }}
    >
      {task.description && (
        <Box
          sx={{
            maxHeight: 360,
            overflowY: 'auto',
            pr: 0.5,
            '&::-webkit-scrollbar': { width: 4 },
            '&::-webkit-scrollbar-track': { bgcolor: 'transparent' },
            '&::-webkit-scrollbar-thumb': {
              bgcolor: (t) => alpha(t.palette.text.primary, 0.15),
              borderRadius: 0.5,
            },
            '& .md-body': {
              fontSize: '0.78rem',
              color: 'text.secondary',
              lineHeight: 1.65,
              '& h1, & h2, & h3, & h4': {
                fontSize: '0.84rem',
                fontWeight: 600,
                color: 'text.primary',
                mt: 1.25,
                mb: 0.5,
              },
              '& p': { m: 0, mb: 0.75 },
              '& ul, & ol': { pl: 2.25, m: 0, mb: 0.75 },
              '& li': { mb: 0.25 },
              '& code': {
                fontFamily: 'monospace',
                fontSize: '0.72rem',
                bgcolor: (t) => alpha(t.palette.text.primary, 0.06),
                px: 0.5,
                py: 0.125,
                borderRadius: 0.75,
              },
              '& pre': {
                bgcolor: (t) => alpha(t.palette.text.primary, 0.04),
                p: 1,
                borderRadius: 1.5,
                overflowX: 'auto',
                mb: 0.75,
                '& code': { bgcolor: 'transparent', p: 0 },
              },
              '& strong': { fontWeight: 600, color: 'text.primary' },
              '& a': { color: 'primary.main' },
              '& blockquote': {
                borderLeft: '3px solid',
                borderColor: 'divider',
                pl: 1.25,
                ml: 0,
                color: 'text.disabled',
              },
            },
          }}
        >
          <Box className="md-body">
            <ReactMarkdown>{task.description}</ReactMarkdown>
          </Box>
        </Box>
      )}

      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 1.5,
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
          <Typography variant="caption" sx={{ color: 'text.disabled', fontWeight: 500 }}>Assignee</Typography>
          <AssigneeChip assignee={task.assignee} />
        </Box>

        <Box sx={{ flex: 1 }} />

        {/* F3c — Retry surface for terminal failure states. Re-dispatches the
            task with a fresh run + freshly minted bearer; the agent pushes
            new commits to the same draft PR.
            - `verification_failed`: tech-lead review caught an issue.
            - `failed`: coding-agent pod itself errored (new-path Job
              terminal Failed, e.g. missing env, image pull, OOM). Both
              transitions go through dispatchSvc.RetryTask which clears
              LastCodingAgentRunName + DispatchedAt so the next dispatch
              creates a new Job. */}
        {(task.status === 'verification_failed' || task.status === 'failed') && task.componentTaskId && (
          <Button
            variant="contained"
            size="small"
            color="warning"
            startIcon={isRetrying ? <CircularProgress size={12} color="inherit" /> : <RotateCcw size={12} />}
            disabled={isRetrying}
            onClick={handleRetry}
          >
            {isRetrying ? 'Retrying…' : 'Retry'}
          </Button>
        )}

        {/* resource-provisioning / config-collection tasks are resolved in the
            architecture-page drawer — deep-link there (Provision / Provide
            configuration) instead of the no-op exec endpoint. */}
        {needsDrawerAction && (
          <Button
            variant="contained"
            size="small"
            startIcon={isOpeningDrawer ? <CircularProgress size={12} color="inherit" /> : <ExternalLink size={12} />}
            disabled={isOpeningDrawer}
            onClick={openDrawer}
          >
            {drawerCtaLabel}
          </Button>
        )}

        {/* Execute Now fires the per-task /tasks/{id}/exec endpoint. It is
            gated to SYSTEM tasks, but excludes the drawer-resolved types above
            (resource-provisioning / config-collection) which have a dedicated
            CTA. Pre-dispatch states only. */}
        {task.componentTaskId && task.execType === 'SYSTEM' && !drawerDep && (!task.status || task.status === 'pending' || task.status === 'on_hold') && (
          task.status === 'on_hold' ? (
            <Tooltip title="Waiting on prerequisite tasks to complete">
              <span>
                <Button
                  variant="contained"
                  size="small"
                  startIcon={<Play size={12} />}
                  disabled
                >
                  Execute Now
                </Button>
              </span>
            </Tooltip>
          ) : (
            <Button
              variant="contained"
              size="small"
              startIcon={isExecuting ? <CircularProgress size={12} color="inherit" /> : <Play size={12} />}
              disabled={isExecuting}
              onClick={handleExecute}
            >
              {isExecuting ? 'Executing…' : 'Execute Now'}
            </Button>
          )
        )}
      </Box>
    </Box>
    <DependencyDrawer
      open={!!drawerDepRef}
      depRef={drawerDepRef}
      orgHandle={orgId}
      projectId={projectId}
      onClose={() => setDrawerDepRef(null)}
      onChanged={refreshDrawerDep}
    />
    </>
  );
}
