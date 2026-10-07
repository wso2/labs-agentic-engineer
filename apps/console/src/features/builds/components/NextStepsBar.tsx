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

import { useNavigate } from "@tanstack/react-router";
import { Alert, Box, Button, Typography } from "@wso2/oxygen-ui";
import { chatStore } from "../../agent-chat/useProjectChat";
import { useStartInterview } from "../../agent-chat/useStartInterview";
import { useOpenSpecTarget } from "../../spec/useSpecWorkspace";
import { useStartFix } from "../api/builds";
import { useNextInterview } from "../hooks/useNextInterview";
import { startedNote, type NextStep, type NextSteps } from "../model/nextSteps";

/** What a finished build offers next, as buttons: the primary step first. */
export function NextStepsBar({ projectName, next }: { projectName: string; next: NextSteps }) {
  const navigate = useNavigate();
  const openSpec = useOpenSpecTarget(projectName);
  const interview = useStartInterview(projectName);
  const nextFeature = useNextInterview(projectName);
  const fix = useStartFix(projectName);
  // The first step is the primary one.
  const primary = next.steps[0];

  const act = (step: NextStep) => {
    switch (step.kind) {
      case "fix":
        fix.mutate(
          step.version,
          {
            onSuccess: (tag) => {
              const version = tag || `${step.version}.1`;
              void navigate({ to: "/projects/$projectName/builds/$version", params: { projectName, version } });
              const note = startedNote(version, [], { stories: step.stories });
              chatStore.post(projectName, note.text, note.actions);
            },
          },
        );
        return;
      case "story":
        openSpec({ file: step.story.split(".")[0] ?? step.story, at: step.story });
        return;
      case "open":
        void navigate({ to: "/projects/$projectName/builds/$version", params: { projectName, version: step.version } });
        return;
      case "validation":
        void navigate({ to: "/projects/$projectName/validations/$version", params: { projectName, version: step.version } });
        return;
      case "interview":
        if (nextFeature?.id === step.featureId) interview.start(nextFeature);
        return;
    }
  };

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
        {next.note && (
          <Typography variant="body2" color="text.secondary">
            {next.note}
          </Typography>
        )}
        {next.steps.map((step) => (
          <Button
            key={step.kind}
            size="small"
            variant={step === primary ? "contained" : "outlined"}
            loading={step.kind === "fix" && fix.isPending}
            disabled={step.kind === "interview" && !interview.ready}
            onClick={() => act(step)}
          >
            {step.label}
          </Button>
        ))}
      </Box>
      {fix.error && <Alert severity="error">{fix.error.message}</Alert>}
    </Box>
  );
}
