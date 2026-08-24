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
  Alert,
  Box,
  Button,
  Card,
  CardActionArea,
  CardContent,
  Chip,
  Grid,
  PageContent,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import {
  ArrowLeft,
  Bot,
  ClipboardCheck,
  GitHub,
  ReceiptText,
} from "@wso2/oxygen-ui-icons-react";
import { useNavigate } from "@tanstack/react-router";
import { ApiRequestError } from "../../../api/errors";
import {
  useCreateProject,
  useGithubOrg,
  useUploadReferences,
} from "../api/queries";
import { isValidProjectName, suggestProjectName } from "../lib/projectName";
import { referenceTypeLabel } from "../lib/referenceFiles";
import { PromptComposer } from "./PromptComposer";

// Issue #71 decision: clicking an example acts as prompt + Start in one
// click — it jumps straight to the name/repo confirmation step.
//
// The examples are the fastest answer a newcomer gets to "what does enough
// detail look like", so they carry the persona (#561): internal enterprise
// work, not consumer apps. The third builds an agent on purpose — Agentic
// Engineer does that too, and this is where it gets advertised.
const EXAMPLE_PROMPTS = [
  {
    icon: <ReceiptText size={24} />,
    title: "Expense approval",
    prompt:
      "Employees submit expense claims, managers approve them, and finance exports approved claims to payroll",
  },
  {
    icon: <ClipboardCheck size={24} />,
    title: "Employee onboarding",
    prompt:
      "Track each new hire's onboarding tasks across IT, HR and facilities, with reminders for overdue items",
  },
  {
    icon: <Bot size={24} />,
    title: "Triage agent",
    prompt:
      "A support triage agent that reads incoming tickets, classifies them by urgency, and drafts replies for a human to approve",
  },
] as const;

function ExampleCard({
  icon,
  title,
  prompt,
  onPick,
}: (typeof EXAMPLE_PROMPTS)[number] & { onPick: (prompt: string) => void }) {
  return (
    <Card variant="outlined" sx={{ height: "100%" }}>
      {/* An explicit flex column, top-aligned. The cards are equal height, but
          a shorter prompt leaves its CardContent shorter than the card and it
          ends up vertically centred — so the card with the longest prompt sat
          9px higher than its neighbours and the three titles never lined up.
          `alignItems` alone does nothing here: CardActionArea is display:block. */}
      <CardActionArea
        sx={{
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "flex-start",
          alignItems: "stretch",
        }}
        onClick={() => onPick(prompt)}
      >
        <CardContent>
          {/* subtitle2, and titles kept to two short words: at subtitle1 a
              third-width card wrapped "Employee onboarding" onto two lines, so
              the three cards' body text started at different heights. */}
          <Stack direction="row" spacing={1.5} sx={{ alignItems: "center", mb: 1 }}>
            {icon}
            <Typography variant="subtitle2" noWrap>
              {title}
            </Typography>
          </Stack>
          <Typography variant="body2" color="text.secondary">
            {prompt}
          </Typography>
        </CardContent>
      </CardActionArea>
    </Card>
  );
}

export function ProjectCreate() {
  const navigate = useNavigate();
  const [step, setStep] = useState<"prompt" | "confirm">("prompt");
  const [prompt, setPrompt] = useState("");
  const [name, setName] = useState("");
  // The repo name follows the project name until the user edits it (#71
  // feedback: repo name is changeable, the org is fixed).
  const [repoName, setRepoName] = useState("");
  const [repoTouched, setRepoTouched] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  // Set once POST /projects succeeds: from that point the project exists, so
  // Back is closed off and the primary action can only be the reference
  // upload's retry (#383 decision: a failed upload is never a failed create).
  const [createdName, setCreatedName] = useState<string | null>(null);
  const { data: githubOrg } = useGithubOrg();
  const createProject = useCreateProject();
  const uploadReferences = useUploadReferences();

  const start = (chosenPrompt: string) => {
    const suggested = suggestProjectName(chosenPrompt);
    setPrompt(chosenPrompt);
    setName(suggested);
    setRepoName(suggested);
    setRepoTouched(false);
    createProject.reset();
    setStep("confirm");
  };

  const changeName = (value: string) => {
    setName(value);
    if (!repoTouched) setRepoName(value);
  };

  const invalidNameMessage =
    "Lowercase letters, digits, and dashes; must start with a letter.";
  const nameError =
    name && !isValidProjectName(name) ? invalidNameMessage : null;
  const repoError =
    repoName && !isValidProjectName(repoName) ? invalidNameMessage : null;

  // A taken repository name is the one create failure the user can fix in
  // place, and it is not recoverable by retrying — the BFF compensates the
  // OpenChoreo project away and fails, so they must pick another name. It
  // belongs on the field, not in a page-level Alert; every other failure keeps
  // the Alert. Branching on the envelope's `code` rather than the message,
  // which the BFF owns and may reword.
  const repoConflict =
    createProject.error instanceof ApiRequestError &&
    createProject.error.code === "conflict"
      ? `That repository name already exists in ${githubOrg ?? "your organization"} — pick another.`
      : null;

  const goToProject = (projectName: string) => {
    void navigate({
      to: "/projects/$projectName",
      params: { projectName },
    });
  };

  const uploadFor = (projectName: string) => {
    uploadReferences.mutate(
      { projectName, files },
      { onSuccess: () => goToProject(projectName) },
    );
  };

  const accept = () => {
    // The project already exists — only the reference upload failed, so the
    // primary action retries just that.
    if (createdName) {
      uploadFor(createdName);
      return;
    }
    createProject.mutate(
      { name, prompt, ...(repoName !== name && { repoName }) },
      {
        onSuccess: (project) => {
          // No client-side copy of the prompt: the BE persists it into the
          // project's own descriptor (specs/.agentic-engineer.toml) on create,
          // and `/start` reads it back from there — so the idea survives a
          // different browser, device, or teammate.
          if (files.length === 0) {
            goToProject(project.name);
            return;
          }
          // Attached reference documents go up separately, to the project's
          // off-git reference store (#383/ADR-0017) — never a commit.
          setCreatedName(project.name);
          uploadFor(project.name);
        },
      },
    );
  };

  const pending = createProject.isPending || uploadReferences.isPending;

  return (
    <PageContent>
      <Box sx={{ maxWidth: 720, mx: "auto", pt: { xs: 4, md: 8 } }}>
        {step === "prompt" ? (
          <Stack spacing={4}>
            <Box sx={{ textAlign: "center" }}>
              <Typography variant="h4" gutterBottom>
                What do you want to build?
              </Typography>
              <Typography variant="body1" color="text.secondary">
                Describe it in your own words — rough is fine, or upload a
                product requirements document.
              </Typography>
            </Box>
            <PromptComposer
              prompt={prompt}
              onPromptChange={setPrompt}
              files={files}
              onFilesChange={setFiles}
              onSubmit={() => start(prompt.trim())}
            />
            <Grid container spacing={2}>
              {EXAMPLE_PROMPTS.map((example) => (
                <Grid key={example.title} size={{ xs: 12, sm: 4 }}>
                  <ExampleCard {...example} onPick={start} />
                </Grid>
              ))}
            </Grid>
          </Stack>
        ) : (
          <Stack spacing={3}>
            <Box>
              <Typography variant="h4" gutterBottom>
                Name your project
              </Typography>
              {/* Labelled "Prompt:" so the user can see what we do with what
                  they wrote — it is the agent's brief, not just a description
                  we filed. Bare quotes never said that.

                  One line, always: this echo confirms "this is what you asked
                  for" (the same transparency-device role the cropped idea has
                  beside /start, #528) rather than displaying the document. The
                  textarea is multiline with no maxLength, so unclamped it is
                  the one element on this page that can grow without bound and
                  push Create project off the fold. Nothing is lost — the full
                  text is on the title attribute, and Back returns to the
                  textarea still holding it. */}
              <Typography variant="body2" color="text.secondary" noWrap title={prompt}>
                Prompt: {prompt}
              </Typography>
            </Box>
            <TextField
              label="Project name"
              value={name}
              onChange={(e) => changeName(e.target.value)}
              error={Boolean(nameError)}
              helperText={
                nameError ?? "Suggested from your prompt — change it if you like."
              }
              fullWidth
            />
            <TextField
              label="Repository name"
              value={repoName}
              onChange={(e) => {
                setRepoTouched(true);
                setRepoName(e.target.value);
              }}
              error={Boolean(repoError) || Boolean(repoConflict)}
              helperText={
                repoError ??
                repoConflict ??
                "Agentic Engineer creates this repository in your organization. Your specs and source code live here, and it stays yours."
              }
              fullWidth
              slotProps={{
                input: {
                  startAdornment: (
                    <Stack
                      direction="row"
                      spacing={0.75}
                      sx={{ alignItems: "center", mr: 0.5, flexShrink: 0 }}
                    >
                      <GitHub size={16} />
                      <Typography variant="body2" color="text.secondary">
                        github.com/{githubOrg ?? "<your-org>"}/
                      </Typography>
                    </Stack>
                  ),
                },
              }}
            />
            {files.length > 0 && (
              <Box>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                  {/* Not "committed to the project": references are transient
                      turn inputs and never enter the repo (ADR-0017). This is
                      also the last place the user ever sees them listed. */}
                  Reference documents the agents will read:
                </Typography>
                <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
                  {files.map((file) => (
                    <Chip
                      key={file.name}
                      label={`${file.name} · ${referenceTypeLabel(file.name)}`}
                      variant="outlined"
                      size="small"
                    />
                  ))}
                </Box>
              </Box>
            )}
            {createProject.isError && !repoConflict && (
              <Alert severity="error">
                {createProject.error instanceof Error
                  ? createProject.error.message
                  : "Failed to create project"}
              </Alert>
            )}
            {createdName && uploadReferences.isError && (
              <Alert severity="error">
                The project was created, but uploading the reference documents
                failed:{" "}
                {uploadReferences.error instanceof Error
                  ? uploadReferences.error.message
                  : "unknown error"}
              </Alert>
            )}
            <Stack direction="row" spacing={2} sx={{ justifyContent: "flex-end" }}>
              <Button
                startIcon={<ArrowLeft size={18} />}
                onClick={() => setStep("prompt")}
                disabled={pending || Boolean(createdName)}
              >
                Back
              </Button>
              {createdName && uploadReferences.isError && (
                <Button
                  onClick={() => goToProject(createdName)}
                  disabled={pending}
                >
                  Continue without documents
                </Button>
              )}
              <Button
                variant="contained"
                onClick={accept}
                disabled={!name || Boolean(nameError) || pending}
                loading={pending}
              >
                {/* Three states, most specific first: the project exists and
                    only the reference upload failed (#383), the create is in
                    flight (#561), or nothing has happened yet. */}
                {createdName
                  ? "Retry upload"
                  : pending
                    ? "Creating your project…"
                    : "Create project"}
              </Button>
            </Stack>
          </Stack>
        )}
      </Box>
    </PageContent>
  );
}
