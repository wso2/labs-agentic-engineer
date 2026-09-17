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

import { useRef, useState, type PropsWithChildren } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Typography,
} from "@wso2/oxygen-ui";
import { useConfigStatus } from "../../settings/api/queries";
import { OnboardingWizard } from "./OnboardingWizard";

// First-run hard gate (issue #102, ADR-0009): an org whose config is missing
// gitProvider or llm is non-functional, so every route yields to the
// onboarding wizard until both are connected. Keyed on GET /config/status —
// the permission-free sibling of GET /config, since this gate must work
// before the caller's AE permissions are necessarily provisioned (the org's
// very first admin may not hold ae:github-config/ae:model-config yet). The
// org itself always exists (default org, platform guarantee).
export function OnboardingGate({ children }: PropsWithChildren) {
  const status = useConfigStatus();
  // Once the wizard has been shown this session, keep it mounted past the
  // moment config becomes complete — the skills-bootstrap step runs after
  // the last credential lands, and only the wizard's own completion
  // actions ("Go to console" / "Continue anyway") dismiss it.
  const wizardShown = useRef(false);
  const [completed, setCompleted] = useState(false);

  if (status.isPending) {
    return (
      <FullScreen>
        <CircularProgress size={32} />
        <Typography variant="body2" color="text.secondary">
          Loading your organization…
        </Typography>
      </FullScreen>
    );
  }

  if (status.isError) {
    return (
      <FullScreen>
        <Typography variant="h6">Couldn't load your organization</Typography>
        <Typography variant="body2" color="text.secondary">
          {status.error.message}
        </Typography>
        <Button variant="contained" onClick={() => void status.refetch()}>
          Try again
        </Button>
      </FullScreen>
    );
  }

  const incomplete = !status.data.gitProviderConnected || !status.data.llmConnected;
  if (incomplete) wizardShown.current = true;

  if (wizardShown.current && !completed) {
    return (
      <OnboardingWizard
        status={status.data}
        onComplete={() => setCompleted(true)}
      />
    );
  }

  return children;
}

// Full-viewport centering for the gate's own loading/error states,
// mirroring auth/AuthScreen.tsx.
function FullScreen({ children }: PropsWithChildren) {
  return (
    <Box
      sx={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 2,
        bgcolor: "background.default",
      }}
    >
      {children}
    </Box>
  );
}
