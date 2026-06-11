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

import { Outlet, useNavigate, useParams, useLocation } from 'react-router-dom';
import { Box, Stack, Typography, Card, CardContent, useTheme } from '@wso2/oxygen-ui';
import { Github, Key, Settings, ShieldCheck, Sparkles } from '@wso2/oxygen-ui-icons-react';

/**
 * OrgSettingsLayout — Phase 2 PR B settings hub shell.
 *
 * Phase 2 ships only Integrations → GitHub. The hub is shaped to take
 * more sections later (Members, Billing) without re-flow.
 *
 * Route: /organizations/:orgId/settings/* (index → github).
 */
export default function OrgSettingsLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const { orgId } = useParams();
  const theme = useTheme();
  const routeOrgId = orgId ?? 'default';

  const sections = [
    {
      key: 'github',
      label: 'GitHub Integration',
      icon: <Github size={18} />,
      path: `/organizations/${routeOrgId}/settings/github`,
    },
    {
      key: 'anthropic',
      label: 'Anthropic Integration',
      icon: <Key size={18} />,
      path: `/organizations/${routeOrgId}/settings/anthropic`,
    },
    {
      key: 'idp',
      label: 'IDP Integration',
      icon: <ShieldCheck size={18} />,
      path: `/organizations/${routeOrgId}/settings/idp`,
    },
    {
      key: 'skills',
      label: 'Skills',
      icon: <Sparkles size={18} />,
      path: `/organizations/${routeOrgId}/settings/skills`,
    },
  ];

  const isActive = (path: string) => location.pathname.startsWith(path);

  return (
    <Box sx={{ p: 3 }}>
      <Stack direction="row" alignItems="center" gap={1.5} sx={{ mb: 3 }}>
        <Settings size={24} />
        <Typography variant="h4" fontWeight={700}>Settings</Typography>
      </Stack>

      <Stack direction="row" gap={3} alignItems="flex-start">
        {/* Left rail */}
        <Card sx={{ minWidth: 240, flexShrink: 0 }}>
          <CardContent sx={{ p: 1.5 }}>
            <Stack gap={0.5}>
              <Typography variant="overline" color="text.secondary" sx={{ px: 1.5 }}>
                Integrations
              </Typography>
              {sections.map((s) => (
                <Box
                  key={s.key}
                  onClick={() => navigate(s.path)}
                  sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 1.5,
                    px: 1.5,
                    py: 1,
                    borderRadius: 1,
                    cursor: 'pointer',
                    backgroundColor: isActive(s.path)
                      ? theme.palette.action.selected
                      : 'transparent',
                    '&:hover': {
                      backgroundColor: theme.palette.action.hover,
                    },
                  }}
                >
                  {s.icon}
                  <Typography variant="body2" fontWeight={isActive(s.path) ? 600 : 400}>
                    {s.label}
                  </Typography>
                </Box>
              ))}
            </Stack>
          </CardContent>
        </Card>

        {/* Outlet */}
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Outlet />
        </Box>
      </Stack>
    </Box>
  );
}
