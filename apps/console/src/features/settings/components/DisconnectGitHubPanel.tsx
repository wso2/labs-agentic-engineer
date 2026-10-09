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

import { Alert, Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle } from "@wso2/oxygen-ui";
import { useDisconnectGitProvider } from "../api/queries";

/**
 * The Disconnect Panel: confirms dropping the org's GitHub connection. The
 * platform installs no GitHub App, so there is nothing to uninstall. Once
 * disconnected the org has no GitHub, so onboarding takes over the console
 * until one is connected.
 */
export function DisconnectGitHubPanel({ onClose }: { onClose: () => void }) {
  const disconnect = useDisconnectGitProvider();

  return (
    <Dialog open onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Disconnect GitHub?</DialogTitle>
      <DialogContent>
        <DialogContentText>
          Projects relying on this org&apos;s GitHub connection will lose spec and code access until it&apos;s
          reconnected.
        </DialogContentText>
        {disconnect.isError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {disconnect.error.message}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          color="error"
          variant="contained"
          onClick={() => disconnect.mutate(undefined, { onSuccess: onClose })}
          disabled={disconnect.isPending}
        >
          {disconnect.isPending ? "Disconnecting…" : "Disconnect"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
