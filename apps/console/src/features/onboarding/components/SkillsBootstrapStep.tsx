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

import { useEffect, useRef, type ReactNode } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Typography,
} from "@wso2/oxygen-ui";
import { Check, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { useSyncSkills } from "../../skills/api/skills";
import { useAeStudio } from "../../ae-studio/api/queries";

// The model connect just before this step rolls the org's AE Studio, so the
// step waits inline for it to be ready before bootstrapping. It releases only
// on a settled answer: while a read is in flight the cached state may predate
// the connect (the config write invalidates it, and a `ready` from before a
// disconnect would otherwise start the sync against a rolling pod). Once
// released (ready, absent, or a read the step cannot act on), it stays on the
// bootstrap for the rest of the step: a later restart must not unmount a sync
// mid-flight.
export function SkillsBootstrapStep({ onComplete }: { onComplete: () => void }) {
  const studio = useAeStudio();
  const state = studio.data?.state;
  const released = useRef(false);
  if (
    !studio.isPending &&
    !studio.isFetching &&
    state !== "provisioning" &&
    state !== "failed"
  ) {
    released.current = true;
  }

  let content: ReactNode;
  if (released.current) {
    content = <SkillsBootstrap onComplete={onComplete} />;
  } else if (state === "failed") {
    content = (
      <StepError
        message="AE Studio couldn't start"
        onRetry={() => void studio.refetch()}
        retrying={studio.isFetching}
        onContinue={onComplete}
      >
        Your skills catalogue can't be set up until it does.
      </StepError>
    );
  } else {
    content = (
      <>
        <CircularProgress size={32} />
        <Typography variant="body2" color="text.secondary">
          Getting AE Studio ready…
        </Typography>
      </>
    );
  }

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
      {content}
    </Box>
  );
}

// Auto-runs the skills bootstrap the moment it mounts (#102 decision):
// POST /skills/sync creates the org's skills repo if missing and pushes the
// platform's built-in skills. Failure is non-blocking — sync is idempotent
// (Retry) and the Skills page's Take updates is the standing fallback
// (Continue anyway).
function SkillsBootstrap({ onComplete }: { onComplete: () => void }) {
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

  if (sync.isError) {
    return (
      <StepError
        message={sync.error.message}
        onRetry={() => sync.mutate()}
        retrying={sync.isPending}
        onContinue={onComplete}
      >
        The skills catalogue couldn't be set up.
      </StepError>
    );
  }

  if (sync.isSuccess) {
    return (
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
    );
  }

  return (
    <>
      <CircularProgress size={32} />
      <Typography variant="body2" color="text.secondary">
        Setting up your skills catalogue — creating the repository and
        installing the platform's built-in skills…
      </Typography>
    </>
  );
}

// The step's error area: what went wrong, why it matters (children), then
// Retry or carry on without skills (the Skills page's Take updates is the
// standing fallback).
function StepError({
  message,
  onRetry,
  retrying = false,
  onContinue,
  children,
}: {
  message: string;
  onRetry: () => void;
  retrying?: boolean;
  onContinue: () => void;
  children: ReactNode;
}) {
  return (
    <>
      <Alert severity="error" sx={{ width: "100%", textAlign: "left" }}>
        {message}
      </Alert>
      <Typography variant="body2" color="text.secondary">
        {children} You can retry now, or continue and use{" "}
        <strong>Take updates</strong> on the Skills page later — agents won't
        have skills until it succeeds.
      </Typography>
      <Box sx={{ display: "flex", gap: 1.5 }}>
        <Button variant="contained" onClick={onRetry} disabled={retrying}>
          Retry
        </Button>
        <Button variant="text" onClick={onContinue}>
          Continue anyway
        </Button>
      </Box>
    </>
  );
}
