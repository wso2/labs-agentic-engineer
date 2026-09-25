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
  Button,
  Chip,
  Dialog,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import {
  displayScopes,
  scopesButtonLabel,
  scopesButtonText,
} from "../lib/testUserScopes";

/** What a login with no scopes renders as. The wire's empty array means the
 *  identity provider could not be asked, so the cell must not read as an
 *  account that may do nothing — and it gets no button, because there is
 *  nothing for a dialog to list. */
export const UNKNOWN_SCOPES = "—";

/** The em dash carries the meaning visually; a screen reader needs the word. */
const UNKNOWN_SCOPES_SPOKEN = "Scopes unknown";

// The standard clip-rect recipe: out of view, still in the accessibility tree.
const VISUALLY_HIDDEN = {
  position: "absolute",
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
  border: 0,
} as const;

/**
 * The Scopes cell: a count and a way in.
 *
 * Scopes are long monospace handles and there can be many. Listed inline they
 * made a six-scope row roughly 350px tall — three accounts filled the viewport
 * — and any inline fold is fragile the moment the agent chat panel opens and
 * narrows the column. Cross-row scanning is what the Role column is for; the
 * scopes themselves are detail on demand, so they open in a dialog and every
 * row stays one line whatever the account holds.
 */
export function TestUserScopesCell({
  username,
  scopes,
}: {
  username: string;
  scopes: readonly string[];
}) {
  const [open, setOpen] = useState(false);

  if (scopes.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">
        <Box component="span" aria-hidden>
          {UNKNOWN_SCOPES}
        </Box>
        <Box component="span" sx={VISUALLY_HIDDEN}>
          {UNKNOWN_SCOPES_SPOKEN}
        </Box>
      </Typography>
    );
  }

  return (
    <>
      <Button
        size="small"
        variant="text"
        aria-label={scopesButtonLabel(scopes.length, username)}
        onClick={() => {
          setOpen(true);
        }}
      >
        {scopesButtonText(scopes.length)}
      </Button>
      <TestUserScopesDialog
        open={open}
        onClose={() => {
          setOpen(false);
        }}
        username={username}
        scopes={scopes}
      />
    </>
  );
}

/** One account's scopes, in a dialog titled for the account they belong to. */
export function TestUserScopesDialog({
  open,
  onClose,
  username,
  scopes,
}: {
  open: boolean;
  onClose: () => void;
  username: string;
  scopes: readonly string[];
}) {
  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ pr: 6 }}>
        Scopes — {username}
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          What this login&apos;s access token carries — the union of the grants
          on every role it holds.
        </Typography>
        <IconButton
          aria-label="Close"
          onClick={onClose}
          size="small"
          sx={{ position: "absolute", right: 12, top: 12 }}
        >
          <X size={18} />
        </IconButton>
      </DialogTitle>
      <DialogContent dividers>
        <Stack direction="row" spacing={0.75} sx={{ flexWrap: "wrap", rowGap: 1 }}>
          {displayScopes(scopes).map((scope) => (
            <Chip
              key={scope}
              size="small"
              variant="outlined"
              label={scope}
              sx={{ fontFamily: "monospace" }}
            />
          ))}
        </Stack>
      </DialogContent>
    </Dialog>
  );
}
