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

import { Avatar, CircularProgress, Paper, Stack, Tooltip, Typography } from '@wso2/oxygen-ui';
import { Users } from '@wso2/oxygen-ui-icons-react';
import type { CollabPeer } from '../hooks/useCollabEditor';

interface Props {
  connected: boolean;
  peers: CollabPeer[];
  inToolbar?: boolean;
}

function peerInitials(name: string): string {
  return name.split(' ').map(w => w[0] ?? '').join('').toUpperCase().slice(0, 2);
}

export default function CollabAwarenessBar({ connected, peers, inToolbar = false }: Props) {
  const editingPeers = peers.filter(p => p.editing);
  const totalPeers = editingPeers.length + 1;

  return (
    <Paper
      variant="outlined"
      sx={{
        px: inToolbar ? 0 : 2,
        py: inToolbar ? 0 : 1,
        mb: inToolbar ? 0 : 2,
        borderRadius: inToolbar ? 0 : 2,
        border: inToolbar ? 'none' : undefined,
        bgcolor: inToolbar ? 'transparent' : undefined,
        display: 'flex',
        alignItems: 'center',
        gap: inToolbar ? 1 : 2,
      }}
    >
      <Stack direction="row" alignItems="center" gap={1}>
        <Users size={inToolbar ? 14 : 16} />
        {!connected ? (
          <>
            <CircularProgress size={14} />
            <Typography variant={inToolbar ? 'caption' : 'body2'} color="text.secondary">Connecting...</Typography>
          </>
        ) : (
          <>
            <Typography variant={inToolbar ? 'caption' : 'body2'} color="text.secondary">
              {totalPeers === 1 ? 'Only you' : `${totalPeers} people`} editing
            </Typography>
            <Stack direction="row" sx={{ ml: inToolbar ? 0.5 : 1 }}>
              {editingPeers.map(peer => (
                <Tooltip key={peer.clientId} title={peer.name}>
                  <Avatar
                    sx={{
                      width: inToolbar ? 22 : 28,
                      height: inToolbar ? 22 : 28,
                      fontSize: inToolbar ? '0.62rem' : '0.7rem',
                      bgcolor: peer.color, ml: -0.5, border: '2px solid white',
                    }}
                  >
                    {peerInitials(peer.name)}
                  </Avatar>
                </Tooltip>
              ))}
            </Stack>
          </>
        )}
      </Stack>
    </Paper>
  );
}
