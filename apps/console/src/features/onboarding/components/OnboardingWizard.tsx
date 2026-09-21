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

import {
  Box,
  Button,
  Paper,
  Step,
  StepLabel,
  Stepper,
  Typography,
} from "@wso2/oxygen-ui";
import { useState } from "react";
import type { components } from "../../../generated/aep-api";
import { useSession } from "../../../auth/SessionContext";
import { WorkspaceAuthzStep } from "./WorkspaceAuthzStep";
import { GitHubStep } from "./GitHubStep";
import { AnthropicStep } from "./AnthropicStep";
import { RepositorySetupStep } from "./RepositorySetupStep";

type ConfigStatus = components["schemas"]["ConfigStatus"];

const STEPS = [
  "Configure workspace",
  "Connect GitHub",
  "Connect Anthropic",
  "Set up repository",
];

// The active step (among GitHub/Anthropic/repository setup) derives from
// server state, not local navigation: each successful PATCH /config
// invalidates GET /config/status (see queries.ts) and the wizard advances. A
// partially-configured org therefore resumes at its first incomplete step
// (issue #102 decisions comment). Exported for direct unit testing
// (OnboardingWizard.test.tsx) without rendering.
//
// This is deliberately blind to the workspace-authz gate ahead of it — that
// gate has no server-side status (ensure is idempotent, so it isn't tracked
// as persisted state) and is handled by the caller's local `authzReady`.
export function activeStep(status: ConfigStatus): number {
  if (!status.gitProviderConnected) return 0;
  if (!status.llmConnected) return 1;
  return 2;
}

// Combines the workspace-authz gate with `activeStep` into the Stepper's
// actual index. `authzReady` is local component state, not server status (see
// activeStep's comment) — passed in here so the combination is testable
// without rendering. Exported for direct unit testing.
export function wizardStep(status: ConfigStatus, authzReady: boolean): number {
  return authzReady ? activeStep(status) + 1 : 0;
}

export function OnboardingWizard({
  status,
  onComplete,
}: {
  status: ConfigStatus;
  onComplete: () => void;
}) {
  const { user, signOut } = useSession();
  // The org's OpenChoreo AuthzRole must exist before GitHub/Anthropic Connect
  // can mirror secrets into OC — see WorkspaceAuthzStep. That gate runs first
  // and unconditionally on every mount (cheap once satisfied), ahead of the
  // server-derived step.
  const [authzReady, setAuthzReady] = useState(false);
  const step = wizardStep(status, authzReady);

  return (
    <Box
      sx={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 3,
        p: 3,
        bgcolor: "background.default",
      }}
    >
      <Box sx={{ textAlign: "center" }}>
        <Typography variant="h5" gutterBottom>
          Welcome to Agentic Engineer
        </Typography>
        <Typography variant="body2" color="text.secondary">
          Your organization needs a few connections before agents can work.
        </Typography>
      </Box>

      <Paper variant="outlined" sx={{ p: 4, width: "100%", maxWidth: 640 }}>
        <Stepper activeStep={step} sx={{ mb: 4 }}>
          {STEPS.map((label) => (
            <Step key={label}>
              <StepLabel>{label}</StepLabel>
            </Step>
          ))}
        </Stepper>

        {step === 0 && <WorkspaceAuthzStep onDone={() => setAuthzReady(true)} />}
        {step === 1 && <GitHubStep />}
        {step === 2 && <AnthropicStep />}
        {step === 3 && <RepositorySetupStep onComplete={onComplete} />}
      </Paper>

      <Typography variant="body2" color="text.secondary">
        Signed in as {user.name} ·{" "}
        <Button variant="text" size="small" onClick={signOut}>
          Sign out
        </Button>
      </Typography>
    </Box>
  );
}
