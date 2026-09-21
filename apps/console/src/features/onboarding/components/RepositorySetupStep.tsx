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
import { AlertCircle, Check, Folder } from "@wso2/oxygen-ui-icons-react";
import { useSyncSkills } from "../../settings/api/queries";

// "skipped" is distinct from both "done" and "error": the step did not
// succeed, but the user chose to proceed and the org is usable without it.
// Collapsing it into either one loses that — "done" would claim the skills
// landed, "error" would hold the user on a screen they have already left.
type RowStatus = "active" | "done" | "error" | "skipped";

// Repository/skills setup. Workspace authz configuration used to be a first
// phase here (#743), but it has to gate every earlier onboarding step's OC
// writes too (GitHub/Anthropic Connect mirror secrets into OC), not just this
// one — so it now runs as its own wizard step ahead of GitHub/Anthropic
// (WorkspaceAuthzStep) and is guaranteed done by the time this step mounts.
export function RepositorySetupStep({ onComplete }: { onComplete: () => void }) {
  const sync = useSyncSkills();
  const [skillsSkipped, setSkillsSkipped] = useState(false);

  // Auto-fires on mount. Deferred one-shot, not a bare mutate() in the
  // effect: firing synchronously binds the mutation's result delivery to the
  // StrictMode-doubled subscription React is about to tear down — the mutation
  // succeeds in the cache but this component never re-renders (stuck spinner).
  // The cleanup-cancelled timeout fires exactly once, after the subscription is
  // stable, in both dev and prod.
  const { mutate: syncMutate } = sync;
  useEffect(() => {
    const t = setTimeout(() => syncMutate(), 0);
    return () => clearTimeout(t);
  }, [syncMutate]);

  const skillsDone = sync.isSuccess || skillsSkipped;

  // `skillsSkipped` is read BEFORE `sync.isError`, and the two are different
  // questions. The mutation's error is sticky — "Continue anyway" does not
  // clear it — so an error-first reading would keep the step incomplete for
  // the rest of the session, leaving the user on a screen whose only exit they
  // have already taken. Settled-ness is what gates progress here.
  const skillsStatus: RowStatus = skillsSkipped
    ? "skipped"
    : sync.isError
      ? "error"
      : skillsDone
        ? "done"
        : "active";

  const allDone = skillsStatus === "done" || skillsStatus === "skipped";

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
          status={skillsStatus}
          label="Create repository"
          description="Creating your organization's skills repository"
        />
      </Box>

      {!allDone && skillsStatus !== "error" && (
        <Typography variant="caption" color="text.secondary" sx={{ textAlign: "center" }}>
          This usually takes a few seconds.
        </Typography>
      )}

      {/* Withdrawn once skipped: the error is sticky, so leaving the panel up
          would offer a choice the user has already made, next to the button
          that acts on it. */}
      {sync.isError && !skillsSkipped && (
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
}: {
  status: RowStatus;
  label: string;
  description: string;
}) {
  return (
    <Box sx={{ display: "flex", alignItems: "flex-start", gap: 1.5, px: 2, py: 1.75 }}>
      <Box sx={{ display: "flex", pt: "1px", flexShrink: 0 }}>
        {status === "done" && (
          <Check size={18} color="var(--oxygen-palette-success-main, currentColor)" />
        )}
        {status === "active" && <CircularProgress size={18} thickness={5} />}
        {status === "error" && (
          <AlertCircle size={18} color="var(--oxygen-palette-error-main, currentColor)" />
        )}
        {status === "skipped" && (
          <Box sx={{ color: "text.secondary", display: "flex" }}>
            <AlertCircle size={18} />
          </Box>
        )}
      </Box>
      <Box>
        <Typography variant="body2">{label}</Typography>
        <Typography variant="caption" color="text.secondary">
          {description}
        </Typography>
      </Box>
    </Box>
  );
}
