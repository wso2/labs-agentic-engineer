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
import type { components } from "../../../generated/aep-api";
import { useSession } from "../../../auth/SessionContext";
import { GitHubStep } from "./GitHubStep";
import { AiAgentsStep } from "./AiAgentsStep";
import { SkillsBootstrapStep } from "./SkillsBootstrapStep";

type ConfigProjection = components["schemas"]["ConfigProjection"];

const STEPS = ["Connect GitHub", "Set up AI agents", "Set up skills"];

// The active step derives from server state, not local navigation: each
// successful PATCH /config updates the query cache and the wizard advances.
// A partially-configured org therefore resumes at its first incomplete step
// (issue #102 decisions comment).
function activeStep(config: ConfigProjection): number {
  if (config.gitProvider === null) return 0;
  if (config.llm === null) return 1;
  return 2;
}

export function OnboardingWizard({
  config,
  onComplete,
}: {
  config: ConfigProjection;
  onComplete: () => void;
}) {
  const { user, signOut } = useSession();
  const step = activeStep(config);

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

        {step === 0 && <GitHubStep />}
        {step === 1 && <AiAgentsStep config={config} />}
        {step === 2 && <SkillsBootstrapStep onComplete={onComplete} />}
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
