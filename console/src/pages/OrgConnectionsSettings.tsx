// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

/**
 * Org-level Connections catalog. Lists every registered external connection
 * (the reusable org-wide definition the architect discovers via MCP and projects
 * provision values into), shows which project/components consume each, and lets
 * an operator delete a connection — but only when nothing uses it (the server
 * enforces the same guard, returning 409 otherwise).
 */

import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Typography,
} from '@wso2/oxygen-ui';
import { Plug, Lock, Trash2, CheckCircle } from '@wso2/oxygen-ui-icons-react';

import {
  listConnections,
  deleteConnection,
  type RegisteredConnection,
} from '../services/api/connections';
import { ApiError } from '../services/api/rest';

export default function OrgConnectionsSettings(): React.ReactElement {
  const { orgId } = useParams();
  const orgHandle = orgId ?? 'default';

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [connections, setConnections] = useState<RegisteredConnection[]>([]);
  const [pendingDelete, setPendingDelete] = useState<RegisteredConnection | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setConnections(await listConnections(orgHandle));
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load connections.');
    } finally {
      setLoading(false);
    }
  }, [orgHandle]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return (
      <Box sx={{ p: 4, display: 'flex', justifyContent: 'center' }}>
        <Typography>Loading connections…</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 1 }}>
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 1 }}>
        <Plug size={22} />
        <Typography variant="h5">Connections</Typography>
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        External connections registered for this organization. Each is defined once and reused by
        any project that declares it. A connection can be deleted only when no component uses it.
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} action={<Button onClick={() => void load()}>Retry</Button>}>
          {error}
        </Alert>
      )}

      {connections.length === 0 ? (
        <Alert severity="info">
          No connections registered yet. They appear here once a project&apos;s design declares an
          <code> external </code> dependency.
        </Alert>
      ) : (
        <Stack spacing={2}>
          {connections.map((c) => {
            const inUse = c.consumers.length > 0;
            return (
              <Card key={c.name} variant="outlined">
                <CardContent>
                  <Stack direction="row" justifyContent="space-between" alignItems="flex-start">
                    <Box sx={{ pr: 2, minWidth: 0 }}>
                      <Typography variant="h6">{c.name}</Typography>
                      {c.description && (
                        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                          {c.description}
                        </Typography>
                      )}
                      <Stack direction="row" spacing={1} sx={{ mt: 1, flexWrap: 'wrap' }}>
                        {c.configKeys.map((k) => (
                          <Chip
                            key={k.key}
                            size="small"
                            variant="outlined"
                            icon={k.secret ? <Lock size={12} /> : undefined}
                            label={k.key}
                          />
                        ))}
                      </Stack>
                      <Box sx={{ mt: 1.5 }}>
                        {inUse ? (
                          <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', alignItems: 'center' }}>
                            <Typography variant="caption" color="text.secondary">
                              Used by:
                            </Typography>
                            {c.consumers.map((u) => (
                              <Chip
                                key={`${u.projectId}/${u.componentName}`}
                                size="small"
                                color="primary"
                                label={`${u.projectId} / ${u.componentName}`}
                              />
                            ))}
                          </Stack>
                        ) : (
                          <Chip size="small" color="success" icon={<CheckCircle size={14} />} label="Not used — deletable" />
                        )}
                      </Box>
                    </Box>
                    <Button
                      variant="outlined"
                      color="error"
                      startIcon={<Trash2 size={16} />}
                      disabled={inUse}
                      onClick={() => setPendingDelete(c)}
                      sx={{ flexShrink: 0 }}
                    >
                      Delete
                    </Button>
                  </Stack>
                </CardContent>
              </Card>
            );
          })}
        </Stack>
      )}

      {pendingDelete && (
        <DeleteDialog
          orgHandle={orgHandle}
          connection={pendingDelete}
          onClose={() => setPendingDelete(null)}
          onDeleted={() => {
            setPendingDelete(null);
            void load();
          }}
        />
      )}
    </Box>
  );
}

interface DeleteDialogProps {
  orgHandle: string;
  connection: RegisteredConnection;
  onClose: () => void;
  onDeleted: () => void;
}

function DeleteDialog({ orgHandle, connection, onClose, onDeleted }: DeleteDialogProps): React.ReactElement {
  const [deleting, setDeleting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const run = async () => {
    setDeleting(true);
    setErr(null);
    try {
      await deleteConnection(orgHandle, connection.name);
      onDeleted();
    } catch (e) {
      const msg =
        e instanceof ApiError ? `${e.message}` : e instanceof Error ? e.message : 'Delete failed';
      setErr(msg);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog open onClose={deleting ? undefined : onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Delete “{connection.name}”?</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary">
          This removes the org-level registration. Its stored secret values and the shared resource
          type are not affected; any project that re-declares it will register it again.
        </Typography>
        {err && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {err}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={deleting}>
          Cancel
        </Button>
        <Button color="error" variant="contained" onClick={() => void run()} disabled={deleting}>
          {deleting ? 'Deleting…' : 'Delete'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
