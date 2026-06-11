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
import { Button, CircularProgress, Typography } from '@wso2/oxygen-ui';
import { Github } from '@wso2/oxygen-ui-icons-react';
import { orgGithubApi } from '../services/api/orgGithub';

interface Props {
  orgHandle: string;
}

/**
 * ConnectAppButton — App-mode connect entry.
 *
 * On click: POST /github/connect/start → receive a GitHub OAuth authorize
 * URL → full-page redirect. The connect-state JWT (15-min TTL) carries
 * ocOrgId + actor through OAuth. The callback exchanges the code for a
 * user-token, intersects /user/installations with our App's installs,
 * and either binds directly (1 candidate), redirects to install
 * (0 candidates), or sends to the picker (2+ candidates).
 *
 * Full-page redirect (not popup) — GitHub's auth pages can't be iframed
 * and a popup adds focus/state-loss complexity.
 */
export default function ConnectAppButton({ orgHandle }: Props) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleClick = async () => {
    setLoading(true);
    setError(null);
    try {
      const { authorizeUrl } = await orgGithubApi.startConnect(orgHandle);
      window.location.assign(authorizeUrl);
    } catch (err) {
      const e = err as Error;
      setError(e.message || 'Could not start App connect');
      setLoading(false);
    }
  };

  return (
    <>
      <Button
        variant="contained"
        startIcon={loading ? <CircularProgress size={16} /> : <Github size={18} />}
        onClick={handleClick}
        disabled={loading}
        size="large"
      >
        {loading ? 'Starting…' : 'Connect GitHub App'}
      </Button>
      {error && (
        <Typography variant="body2" color="error" sx={{ mt: 1 }}>{error}</Typography>
      )}
    </>
  );
}
