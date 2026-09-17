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

import { useEffect, useState } from "react";
import { Alert, Box, Button, CircularProgress, Typography } from "@wso2/oxygen-ui";
import { Lock } from "@wso2/oxygen-ui-icons-react";
import { useEnsureAuthzRole } from "../../settings/api/queries";

// After this many total attempts (the automatic first one included), the
// failure message escalates from a plain retry prompt to one acknowledging
// the delay — mirrors RepositorySetupStep's AUTHZ_ESCALATE_AFTER_ATTEMPTS.
const ESCALATE_AFTER_ATTEMPTS = 3;

// Runs FIRST, before GitHub/Anthropic connect: the org's OpenChoreo AuthzRole
// must exist before Connect's SM-API mirror can create a SecretReference in
// OC, or that write 403s and silently leaves the credential row without a
// secret ref — surfacing much later as an opaque coding-agent dispatch
// failure ("redispatch-budget") rather than here, where it's actionable.
//
// Ordering used to be the other way round (authz ran last, inside repository
// setup) on the theory that it only gated skills sync. It also gates every
// earlier step's OC writes, so it has to run before all of them, not after.
//
// No server-side "authz ready" status exists (or is needed): ensure is
// idempotent and cheap once the role exists, so re-running it on every wizard
// mount is harmless — it's tracked as local step state, not persisted.
export function WorkspaceAuthzStep({ onDone }: { onDone: () => void }) {
  const authz = useEnsureAuthzRole();
  const [attempts, setAttempts] = useState(0);

  // Deferred one-shot, not a bare mutate() in the effect: firing synchronously
  // binds the mutation's result delivery to the StrictMode-doubled
  // subscription React is about to tear down — the mutation succeeds in the
  // cache but this component never re-renders (stuck spinner). The
  // cleanup-cancelled timeout fires exactly once, after the subscription is
  // stable, in both dev and prod.
  const { mutate } = authz;
  useEffect(() => {
    const t = setTimeout(() => {
      setAttempts((n) => n + 1);
      mutate();
    }, 0);
    return () => clearTimeout(t);
  }, [mutate]);

  const { isSuccess } = authz;
  useEffect(() => {
    if (isSuccess) onDone();
  }, [isSuccess, onDone]);

  function retry() {
    setAttempts((n) => n + 1);
    authz.mutate();
  }

  const escalated = attempts >= ESCALATE_AFTER_ATTEMPTS;

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3, py: 1 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
        <Lock size={20} />
        <Typography variant="h6">Configure workspace</Typography>
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mt: -2 }}>
        Applying access roles for your organization. This runs before
        anything else so later steps can write to your workspace.
      </Typography>

      {!authz.isError && (
        <Box sx={{ display: "flex", justifyContent: "center", py: 4 }}>
          <CircularProgress size={28} thickness={5} />
        </Box>
      )}

      {authz.isError && (
        <>
          <Alert severity="error">{authz.error.message}</Alert>
          <Typography variant="body2" color="text.secondary">
            {escalated
              ? "This is taking longer than expected. This usually clears on its own — check with your administrator if it persists."
              : "We couldn't finish configuring your workspace. This step is required before you can continue."}
          </Typography>
          <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
            <Button variant="contained" onClick={retry} disabled={authz.isPending}>
              Retry
            </Button>
          </Box>
        </>
      )}
    </Box>
  );
}
