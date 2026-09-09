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
import { AlertCircle, Check, Circle, Folder } from "@wso2/oxygen-ui-icons-react";
import { useEnsureAuthzRole, useSyncSkills } from "../../settings/api/queries";

// After this many total attempts (the automatic first one included), the
// workspace-configuration failure message escalates from a plain retry
// prompt to one acknowledging the delay (#743 decision — capped, not
// unlimited-silent).
const AUTHZ_ESCALATE_AFTER_ATTEMPTS = 3;

type RowStatus = "pending" | "active" | "done" | "error";

// Two independent phases run in sequence (#743, splitting #102's single
// bootstrap step): repository/skills setup (unchanged), then workspace authz
// configuration. Phase 2 is a hard gate — the org's OpenChoreo AuthzRole must
// exist before the console is usable, so unlike phase 1 there is no "Continue
// anyway" for it.
export function RepositorySetupStep({ onComplete }: { onComplete: () => void }) {
  const sync = useSyncSkills();
  const authz = useEnsureAuthzRole();
  const [skillsSkipped, setSkillsSkipped] = useState(false);
  const [authzAttempts, setAuthzAttempts] = useState(0);

  // Phase 1 auto-fires on mount, unchanged from #102's SkillsBootstrapStep.
  // Deferred one-shot, not a bare mutate() in the effect: firing synchronously
  // binds the mutation's result delivery to the StrictMode-doubled
  // subscription React is about to tear down — the mutation succeeds in the
  // cache but this component never re-renders (stuck spinner). The
  // cleanup-cancelled timeout fires exactly once, after the subscription is
  // stable, in both dev and prod.
  const { mutate: syncMutate } = sync;
  useEffect(() => {
    const t = setTimeout(() => syncMutate(), 0);
    return () => clearTimeout(t);
  }, [syncMutate]);

  const phase1Done = sync.isSuccess || skillsSkipped;

  // Phase 2 auto-fires once phase 1 resolves (success or skipped) — same
  // deferred-timeout StrictMode guard as phase 1, keyed on phase1Done instead
  // of mount.
  const { mutate: authzMutate } = authz;
  useEffect(() => {
    if (!phase1Done) return;
    const t = setTimeout(() => {
      setAuthzAttempts((n) => n + 1);
      authzMutate();
    }, 0);
    return () => clearTimeout(t);
  }, [phase1Done, authzMutate]);

  function retryAuthz() {
    setAuthzAttempts((n) => n + 1);
    authz.mutate();
  }

  const phase1Status: RowStatus = sync.isError
    ? "error"
    : phase1Done
      ? "done"
      : "active";
  const phase2Status: RowStatus = !phase1Done
    ? "pending"
    : authz.isError
      ? "error"
      : authz.isSuccess
        ? "done"
        : "active";

  const allDone = phase1Status === "done" && phase2Status === "done";
  const authzEscalated = authzAttempts >= AUTHZ_ESCALATE_AFTER_ATTEMPTS;

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3, py: 1 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
        <Folder size={20} />
        <Typography variant="h6">Set up repository</Typography>
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mt: -2 }}>
        We're creating a dedicated repository for your organization and
        preparing your workspace to run agents.
      </Typography>

      <Box
        sx={{
          border: "1px solid",
          borderColor: "divider",
          borderRadius: 2,
          overflow: "hidden",
        }}
      >
        <ChecklistRow
          status={phase1Status}
          label="Create repository"
          description="Creating your organization's skills repository"
        />
        <ChecklistRow
          status={phase2Status}
          label="Configure workspace"
          description="Applying access roles for your organization"
          divider
        />
      </Box>

      {!allDone && phase1Status !== "error" && phase2Status !== "error" && (
        <Typography variant="caption" color="text.secondary" sx={{ textAlign: "center" }}>
          This usually takes a few seconds.
        </Typography>
      )}

      {sync.isError && (
        <>
          <Alert severity="error">{sync.error.message}</Alert>
          <Typography variant="body2" color="text.secondary">
            The skills catalogue couldn't be set up. You can retry now, or
            continue and run <strong>Sync</strong> from Settings → Skills
            later — agents won't have skills until it succeeds.
          </Typography>
          <Box sx={{ display: "flex", justifyContent: "flex-end", gap: 1.5 }}>
            <Button variant="text" onClick={() => setSkillsSkipped(true)}>
              Continue anyway
            </Button>
            <Button
              variant="contained"
              onClick={() => sync.mutate()}
              disabled={sync.isPending}
            >
              Retry
            </Button>
          </Box>
        </>
      )}

      {authz.isError && (
        <>
          <Alert severity="error">{authz.error.message}</Alert>
          <Typography variant="body2" color="text.secondary">
            {authzEscalated
              ? "This is taking longer than expected. This usually clears on its own — check with your administrator if it persists."
              : "We couldn't finish configuring your workspace. This step is required before you can continue."}
          </Typography>
          <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
            <Button
              variant="contained"
              onClick={retryAuthz}
              disabled={authz.isPending}
            >
              Retry
            </Button>
          </Box>
        </>
      )}

      {allDone && (
        <Box
          sx={{
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: 1,
            textAlign: "center",
          }}
        >
          <Check size={32} color="var(--oxygen-palette-success-main, currentColor)" />
          <Typography variant="body2" color="text.secondary">
            {skillsSkipped
              ? "Your workspace is configured. Skills catalogue setup was skipped — you can retry it from Settings → Skills."
              : "Your repository and workspace are ready to go."}
          </Typography>
          <Button variant="contained" onClick={onComplete}>
            Go to console
          </Button>
        </Box>
      )}
    </Box>
  );
}

function ChecklistRow({
  status,
  label,
  description,
  divider,
}: {
  status: RowStatus;
  label: string;
  description: string;
  divider?: boolean;
}) {
  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "flex-start",
        gap: 1.5,
        px: 2,
        py: 1.75,
        ...(divider && { borderTop: "1px solid", borderColor: "divider" }),
      }}
    >
      <Box sx={{ display: "flex", pt: "1px", flexShrink: 0 }}>
        {status === "done" && (
          <Check size={18} color="var(--oxygen-palette-success-main, currentColor)" />
        )}
        {status === "active" && <CircularProgress size={18} thickness={5} />}
        {status === "pending" && (
          <Box sx={{ color: "text.disabled", display: "flex" }}>
            <Circle size={18} />
          </Box>
        )}
        {status === "error" && (
          <AlertCircle size={18} color="var(--oxygen-palette-error-main, currentColor)" />
        )}
      </Box>
      <Box>
        <Typography
          variant="body2"
          color={status === "pending" ? "text.disabled" : "text.primary"}
        >
          {label}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {description}
        </Typography>
      </Box>
    </Box>
  );
}
