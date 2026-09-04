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
  Link as MuiLink,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ExternalLink, Users } from "@wso2/oxygen-ui-icons-react";
import { env } from "../../../config/env";
import { thunderUsersConsoleHref } from "../../../config/thunderConsole";
import {
  useProjectRoles,
  useRevealTestUserPassword,
} from "../../spec/api/roles";
import {
  publishedTestUsers,
  type PublishedTestUser,
} from "../lib/publishedTestUsers";
import { TestUsersDialog } from "./TestUsersDialog";

function ThunderSentence({ thunderUrl }: { thunderUrl: string }) {
  return (
    <Typography
      variant="caption"
      color="text.secondary"
      sx={{ display: "block" }}
    >
      Manage user accounts in{" "}
      <MuiLink
        href={thunderUsersConsoleHref(thunderUrl)}
        target="_blank"
        rel="noreferrer"
        variant="inherit"
        aria-label="Open Thunder Console to add or remove real accounts"
        sx={{
          display: "inline-flex",
          alignItems: "center",
          gap: 0.5,
        }}
      >
        Thunder Console
        <ExternalLink size={11} aria-hidden />
      </MuiLink>
    </Typography>
  );
}

/**
 * Sign-in facts for a live deployment. Parent mounts only when deploy is green.
 * Empty logins stay silent except for the Thunder Console link.
 */
export function SignInPanel({
  logins,
  thunderUrl,
  revealPassword,
  loadState = "ready",
}: {
  logins: readonly PublishedTestUser[];
  thunderUrl: string;
  revealPassword: (username: string) => Promise<string>;
  loadState?: "ready" | "pending" | "error";
}) {
  const [open, setOpen] = useState(false);
  return (
    <Box>
      {loadState === "pending" && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: "block", mb: 1 }}
        >
          Loading test users…
        </Typography>
      )}
      {loadState === "error" && (
        <Typography
          variant="caption"
          color="error"
          sx={{ display: "block", mb: 1 }}
        >
          Couldn&apos;t load test users.
        </Typography>
      )}
      {loadState === "ready" && logins.length > 0 && (
        <>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: "block" }}
          >
            Test users for agents on this environment
          </Typography>
          {/* One LINE, whatever the design declares. The accounts used to be
              listed here, one two-line block per role, so a six-role app grew
              this card past the ledger beside it (review on #714). The count
              is the fact the card owes the reader; the accounts themselves are
              a table, and a table belongs in a dialog. */}
          <Stack
            direction="row"
            spacing={1.5}
            sx={{ alignItems: "center", mt: 1, mb: 1.5 }}
          >
            <Typography variant="body2">
              {logins.length} account{logins.length === 1 ? "" : "s"}, one per
              role
            </Typography>
            <Button
              size="small"
              variant="outlined"
              startIcon={<Users size={14} aria-hidden />}
              onClick={() => setOpen(true)}
            >
              View test users
            </Button>
          </Stack>
          <TestUsersDialog
            open={open}
            onClose={() => setOpen(false)}
            logins={logins}
            revealPassword={revealPassword}
          />
        </>
      )}
      <ThunderSentence thunderUrl={thunderUrl} />
    </Box>
  );
}

/**
 * Live Deploy wiring for SignInPanel. Mount only when deploy is green so the
 * roles GET does not run while the panel is hidden.
 */
export function ProjectSignInPanel({
  projectName,
}: {
  projectName: string;
}) {
  const live = useProjectRoles(projectName, true);
  const reveal = useRevealTestUserPassword(projectName);
  const loadState = live.isPending
    ? "pending"
    : live.isError
      ? "error"
      : "ready";
  const logins =
    loadState === "ready" ? publishedTestUsers(live.data?.testUsers ?? []) : [];

  return (
    <SignInPanel
      logins={logins}
      thunderUrl={env.thunderUrl}
      loadState={loadState}
      revealPassword={async (username) => {
        const data = await reveal.mutateAsync(username);
        return data.password;
      }}
    />
  );
}
