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

import { useState, useEffect } from 'react';
import { Alert, Button, Stack, Typography } from '@wso2/oxygen-ui';

interface Props {
  ocOrgId: string;
  identityChangedAt: string; // ISO timestamp; used as part of the dismissal key
  prevIdentityLogin?: string;
  identityLogin: string;
}

/**
 * IdentityDriftBanner — Phase 2 PR B (PAT mode only).
 *
 * Renders above the connected-PAT panel when identityChangedAt is set
 * (the PAT was replaced and the new owner differs from the prior). The
 * dismissal is per-(ocOrgId, identityChangedAt) so a future drift
 * re-surfaces a fresh banner.
 *
 * Per phase2.md §10.9.
 */
export default function IdentityDriftBanner({ ocOrgId, identityChangedAt, prevIdentityLogin, identityLogin }: Props) {
  const dismissalKey = `asdlc.identity-drift.dismissed:${ocOrgId}:${identityChangedAt}`;
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    setDismissed(localStorage.getItem(dismissalKey) === '1');
  }, [dismissalKey]);

  const handleDismiss = () => {
    localStorage.setItem(dismissalKey, '1');
    setDismissed(true);
  };

  if (dismissed) return null;

  const date = identityChangedAt ? new Date(identityChangedAt).toLocaleDateString() : '—';
  const prev = prevIdentityLogin ?? 'unknown';

  return (
    <Alert
      severity="warning"
      sx={{ mb: 2 }}
      action={
        <Button color="inherit" size="small" onClick={handleDismiss}>Dismiss</Button>
      }
    >
      <Stack gap={0.5}>
        <Typography variant="subtitle2" fontWeight={600}>
          Identity changed from "{prev}" to "{identityLogin}" on {date}.
        </Typography>
        <Typography variant="body2">
          New commits and PRs will use the new identity.
        </Typography>
      </Stack>
    </Alert>
  );
}
