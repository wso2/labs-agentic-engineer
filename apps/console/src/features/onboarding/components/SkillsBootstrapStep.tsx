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

import { useEffect } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Typography,
} from "@wso2/oxygen-ui";
import { Check, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { useSyncSkills } from "../../settings/api/queries";

// Auto-runs the skills bootstrap the moment credentials complete (#102
// decision): POST /skills/sync creates the org's skills repo if missing and
// pushes the platform's built-in skills. Failure is non-blocking — sync is
// idempotent (Retry) and Settings' Sync control is the standing fallback
// (Continue anyway).
export function SkillsBootstrapStep({ onComplete }: { onComplete: () => void }) {
  const sync = useSyncSkills();
  // Deferred one-shot, not a bare mutate() in the effect: firing a mutation
  // synchronously inside the mount effect binds its result delivery to the
  // StrictMode-doubled subscription React is about to tear down — the
  // mutation succeeds in the cache but this component never re-renders
  // (stuck spinner). The cleanup-cancelled timeout fires exactly once,
  // after the subscription is stable, in both dev and prod.
  const { mutate } = sync;
  useEffect(() => {
    const t = setTimeout(() => mutate(), 0);
    return () => clearTimeout(t);
  }, [mutate]);

  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: 2,
        py: 2,
        textAlign: "center",
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
        <Sparkles size={22} />
        <Typography variant="h6">Set up skills</Typography>
      </Box>

      {sync.isError ? (
        <>
          <Alert severity="error" sx={{ width: "100%", textAlign: "left" }}>
            {sync.error.message}
          </Alert>
          <Typography variant="body2" color="text.secondary">
            The skills catalogue couldn't be set up. You can retry now, or
            continue and run <strong>Sync</strong> from Settings → Skills
            later — agents won't have skills until it succeeds.
          </Typography>
          <Box sx={{ display: "flex", gap: 1.5 }}>
            <Button
              variant="contained"
              onClick={() => sync.mutate()}
              disabled={sync.isPending}
            >
              Retry
            </Button>
            <Button variant="text" onClick={onComplete}>
              Continue anyway
            </Button>
          </Box>
        </>
      ) : sync.isSuccess ? (
        <>
          <Check size={32} />
          <Typography variant="body2" color="text.secondary">
            Your skills catalogue is ready
            {sync.data.updated > 0 ? ` — ${sync.data.updated} built-in skill${
              sync.data.updated === 1 ? "" : "s"
            } installed` : ""}
            . Your organization is all set.
          </Typography>
          <Button variant="contained" onClick={onComplete}>
            Go to console
          </Button>
        </>
      ) : (
        <>
          <CircularProgress size={32} />
          <Typography variant="body2" color="text.secondary">
            Setting up your skills catalogue — creating the repository and
            installing the platform's built-in skills…
          </Typography>
        </>
      )}
    </Box>
  );
}
