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

import { useState } from "react";
import {
  Box,
  CircularProgress,
  Dialog,
  DialogContent,
  DialogTitle,
  IconButton,
  ListingTable,
  Stack,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Copy, Eye, EyeOff, X } from "@wso2/oxygen-ui-icons-react";
import { StatusChip } from "../../../components/StatusChip";
import type { PublishedTestUser } from "../lib/publishedTestUsers";

/** The password's placeholder while it is hidden. Fixed width, monospace, so
 *  revealing swaps the characters without moving the icons beside them. */
export const MASK = "**********";

function copyText(value: string): Promise<void> {
  if (!navigator.clipboard?.writeText) {
    return Promise.reject(new Error("Clipboard is not available"));
  }
  return navigator.clipboard.writeText(value);
}

/**
 * One account: its username, its masked password with the two controls, the
 * role it holds, and whether it is the cold-start account.
 *
 * The reveal state is per row and lives here, so opening one password does not
 * open the rest — the dialog can hold a dozen accounts and only the one asked
 * for is ever on screen.
 */
function TestUserRow({
  login,
  revealPassword,
}: {
  login: PublishedTestUser;
  revealPassword: (username: string) => Promise<string>;
}) {
  const [password, setPassword] = useState<string | null>(null);
  const [shown, setShown] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /**
   * The password itself, read once and kept for the row's life.
   *
   * The eye toggles VISIBILITY, not another read: hiding a revealed password
   * and showing it again is a decision about this screen, not a reason to ask
   * the sealed store for a secret a second time. Copy shares the same path, so
   * a reader can copy a password they never put on screen.
   */
  const ensurePassword = async (): Promise<string | null> => {
    if (password !== null) return password;
    setBusy(true);
    setError(null);
    try {
      const value = await revealPassword(login.username);
      setPassword(value);
      return value;
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return null;
    } finally {
      setBusy(false);
    }
  };

  const toggle = async () => {
    if (shown) {
      setShown(false);
      return;
    }
    if ((await ensurePassword()) !== null) setShown(true);
  };

  const copy = async () => {
    const value = await ensurePassword();
    if (value === null) return;
    setError(null);
    try {
      await copyText(value);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <ListingTable.Row>
      <ListingTable.Cell>
        <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
          {login.username}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Stack direction="row" spacing={0.5} sx={{ alignItems: "center" }}>
          <Typography
            variant="body2"
            aria-live="polite"
            // The mask is decoration: its ten asterisks would be read out one
            // by one, and the eye's accessible name already says whether the
            // password is showing.
            {...(shown && password !== null ? {} : { "aria-hidden": true })}
            sx={{ fontFamily: "monospace", color: "text.secondary" }}
          >
            {shown && password !== null ? password : MASK}
          </Typography>
          <Tooltip title={shown ? "Hide password" : "Reveal password"}>
            <span>
              <IconButton
                size="small"
                disabled={busy}
                aria-label={
                  shown
                    ? `Hide the password for ${login.username}`
                    : `Reveal the password for ${login.username}`
                }
                onClick={() => void toggle()}
              >
                {busy ? (
                  <CircularProgress size={12} color="inherit" />
                ) : shown ? (
                  <EyeOff size={14} />
                ) : (
                  <Eye size={14} />
                )}
              </IconButton>
            </span>
          </Tooltip>
          <Tooltip title="Copy password">
            <span>
              <IconButton
                size="small"
                disabled={busy}
                aria-label={`Copy the password for ${login.username}`}
                onClick={() => void copy()}
              >
                <Copy size={14} />
              </IconButton>
            </span>
          </Tooltip>
        </Stack>
        {error !== null && (
          <Typography variant="caption" color="error" sx={{ display: "block" }}>
            {error}
          </Typography>
        )}
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Typography variant="body2">{login.role}</Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        {login.coldStart ? (
          <Tooltip title="Served when a caller asks for credentials without naming a role">
            <span>
              <StatusChip label="Cold start" tone="info" appearance="soft" />
            </span>
          </Tooltip>
        ) : (
          <Typography variant="body2" color="text.secondary">
            —
          </Typography>
        )}
      </ListingTable.Cell>
    </ListingTable.Row>
  );
}

/**
 * The environment's test accounts, in a dialog.
 *
 * They live behind a button rather than on the Development card because the
 * list is one account PER ROLE: an app with six roles pushed six two-line
 * blocks into a card that sits beside the ledger, and the card grew with the
 * design instead of holding its place (review on #714).
 */
export function TestUsersDialog({
  open,
  onClose,
  logins,
  revealPassword,
}: {
  open: boolean;
  onClose: () => void;
  logins: readonly PublishedTestUser[];
  revealPassword: (username: string) => Promise<string>;
}) {
  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle sx={{ pr: 6 }}>
        Test users
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          Disposable accounts the platform created for this project&apos;s
          roles, so agents can sign in to the running app and check what each
          role can do. They are not real people.
        </Typography>
        <IconButton
          aria-label="Close"
          onClick={onClose}
          sx={{ position: "absolute", right: 12, top: 12 }}
        >
          <X size={18} />
        </IconButton>
      </DialogTitle>
      <DialogContent dividers sx={{ p: 0 }}>
        <ListingTable.Container sx={{ width: "100%" }}>
          <ListingTable density="standard">
            <ListingTable.Head>
              <ListingTable.Row>
                <ListingTable.Cell>Username</ListingTable.Cell>
                <ListingTable.Cell sx={{ width: 260 }}>Password</ListingTable.Cell>
                <ListingTable.Cell sx={{ width: 180 }}>Role</ListingTable.Cell>
                <ListingTable.Cell sx={{ width: 130 }}>Cold start</ListingTable.Cell>
              </ListingTable.Row>
            </ListingTable.Head>
            <ListingTable.Body>
              {logins.map((login) => (
                <TestUserRow
                  key={login.username}
                  login={login}
                  revealPassword={revealPassword}
                />
              ))}
            </ListingTable.Body>
          </ListingTable>
        </ListingTable.Container>
        <Box sx={{ px: 2.25, py: 1.5 }}>
          <Typography variant="caption" color="text.secondary">
            The same logins are posted on this version&apos;s roles gate issue
            when the build provisions them.
          </Typography>
        </Box>
      </DialogContent>
    </Dialog>
  );
}
