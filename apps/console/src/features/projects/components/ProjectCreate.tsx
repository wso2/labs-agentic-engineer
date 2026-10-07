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

import { useRef, useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  Alert,
  Box,
  Button,
  Card,
  CardActionArea,
  Chip,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { GitHub } from "@wso2/oxygen-ui-icons-react";
import { ApiRequestError } from "../../../api/errors";
import { releaseKickoff } from "../../agent-chat/useProjectChat";
import { BasePage } from "../../shell/components/BasePage";
import { PHONE } from "../../shell/layout";
import { useCreateProject, useGithubOrg, useUploadReferences } from "../api/queries";
import { isValidProjectName, suggestProjectName } from "../projectName";
import { referenceTypeLabel } from "../referenceFiles";
import { PromptComposer } from "./PromptComposer";

// The examples are the fastest answer a newcomer gets to "what does enough
// detail look like", so they carry the persona (#561): internal enterprise
// work, not consumer apps. The third builds an agent on purpose: Agentic
// Engineer does that too, and this is where it gets advertised.
const EXAMPLE_PROMPTS = [
  {
    title: "Expense approval",
    prompt:
      "Employees submit expense claims, managers approve them, and finance exports approved claims to payroll",
  },
  {
    title: "Employee onboarding",
    prompt:
      "Track each new hire's onboarding tasks across IT, HR and facilities, with reminders for overdue items",
  },
  {
    title: "Triage agent",
    prompt:
      "A support triage agent that reads incoming tickets, classifies them by urgency, and drafts replies for a human to approve",
  },
] as const;

const INVALID_NAME = "Lowercase letters, digits, and dashes; must start with a letter.";

/** Both steps sit centred in the main area, at their own width. */
function CenterPage({ width, children }: { width: number; children: ReactNode }) {
  return (
    // One track no wider than the page: an auto track would grow to the
    // one-line prompt echo's full length and push the form off screen.
    <Box sx={{ minHeight: "100%", display: "grid", gridTemplateColumns: "minmax(0, 1fr)", placeItems: "center", py: 3 }}>
      <Box sx={{ width: `min(${width}px, 100%)` }}>{children}</Box>
    </Box>
  );
}

function ExampleCard({ title, prompt, onPick }: { title: string; prompt: string; onPick: () => void }) {
  return (
    <Card
      variant="outlined"
      sx={{
        borderRadius: 2.5,
        transition: (t) => t.transitions.create("border-color"),
        "&:hover": { borderColor: "primary.main" },
      }}
    >
      {/* A top-aligned column: CardActionArea is display:block, and a shorter
          prompt would otherwise sit vertically centred beside its neighbours. */}
      <CardActionArea
        onClick={onPick}
        sx={{ height: "100%", px: 1.5, py: 1.25, display: "flex", flexDirection: "column", alignItems: "stretch", justifyContent: "flex-start", gap: 0.5 }}
      >
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {title}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ fontSize: "0.78125rem" }}>
          {prompt}
        </Typography>
      </CardActionArea>
    </Card>
  );
}

/**
 * New project, in two steps: what to build (the prompt and any reference
 * documents), then the project and repository names. Creating hands the prompt
 * to the platform, which starts the kickoff itself, and lands on the project's
 * overview with the chat open.
 */
export function ProjectCreate() {
  const navigate = useNavigate();
  const promptInput = useRef<HTMLTextAreaElement>(null);
  const [step, setStep] = useState<"prompt" | "details">("prompt");
  const [prompt, setPrompt] = useState("");
  const [name, setName] = useState("");
  // The repo name follows the project name until the user edits it (#71: the
  // repo name is changeable, the org is fixed).
  const [repoName, setRepoName] = useState("");
  const [repoTouched, setRepoTouched] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  // Set once POST /projects succeeds: from then on the project exists, so Back
  // is closed off and the primary action can only retry the reference upload
  // (#383: a failed upload is never a failed create).
  const [createdName, setCreatedName] = useState<string | null>(null);
  const githubOrg = useGithubOrg();
  const createProject = useCreateProject();
  const uploadReferences = useUploadReferences();

  // An example fills the prompt and hands focus back to it, so it can be
  // edited or sent on with Continue.
  const pickExample = (example: string) => {
    setPrompt(example);
    promptInput.current?.focus();
  };

  const toDetails = () => {
    const suggested = suggestProjectName(prompt.trim());
    setPrompt(prompt.trim());
    setName(suggested);
    setRepoName(suggested);
    setRepoTouched(false);
    createProject.reset();
    setStep("details");
  };

  const changeName = (value: string) => {
    setName(value);
    if (!repoTouched) setRepoName(value);
  };

  const nameError = name && !isValidProjectName(name) ? INVALID_NAME : null;
  const repoError = repoName && !isValidProjectName(repoName) ? INVALID_NAME : null;

  // A taken name is the one create failure the user can fix in place, and
  // retrying does not help: the BFF compensates the half-made project away and
  // fails, so they must pick another name. It goes on the repository field;
  // every other failure keeps the page-level Alert. Branching on the
  // envelope's `code`, not the message, which the BFF owns and may reword.
  const repoConflict =
    createProject.error instanceof ApiRequestError && createProject.error.code === "conflict"
      ? `That repository name already exists in ${githubOrg ?? "your organization"}. Pick another.`
      : null;

  // The journey is already underway by the time this runs: the platform fired
  // `/start` server-side (#562), so the user lands on the overview with the
  // chat open, their own prompt going in as the first message. The param
  // raises the panel and the shell strips it on arrival, so a refresh later
  // does not reopen it.
  const goToProject = (projectName: string) => {
    void navigate({
      to: "/projects/$projectName",
      params: { projectName },
      search: { chat: "open" as const },
    });
  };

  const uploadFor = (projectName: string) => {
    uploadReferences.mutate({ projectName, files }, { onSuccess: () => goToProject(projectName) });
  };

  // Going on without the documents after their upload failed. The create said
  // documents were coming, so the platform HELD the kickoff for an upload that
  // is now never coming, and nothing else releases it: the project would sit
  // un-started. The chat sends `/start` once it sees the conversation is still
  // empty (guarded, so a kickoff that ran after all is not started twice).
  const continueWithoutDocuments = (projectName: string) => {
    releaseKickoff(projectName);
    goToProject(projectName);
  };

  const create = () => {
    // The project already exists and only the reference upload failed, so the
    // primary action retries just that.
    if (createdName) {
      uploadFor(createdName);
      return;
    }
    createProject.mutate(
      {
        name,
        prompt,
        ...(repoName !== name && { repoName }),
        // Documents are the primary brief, and an interview started before
        // they arrive is conducted blind, so the platform HOLDS the kickoff
        // until the upload lands and fires it from there (#562).
        ...(files.length > 0 && { referencesPending: true }),
      },
      {
        onSuccess: (project) => {
          // No client-side copy of the prompt: the platform persists it into
          // the project's descriptor on create, and `/start` reads it back
          // from there, so the idea survives another browser or teammate.
          if (files.length === 0) {
            goToProject(project.name);
            return;
          }
          setCreatedName(project.name);
          uploadFor(project.name);
        },
      },
    );
  };

  const pending = createProject.isPending || uploadReferences.isPending;
  const uploadFailed = createdName !== null && uploadReferences.isError;

  if (step === "prompt") {
    return (
      <BasePage>
        <CenterPage width={680}>
          <Stack spacing={2.25}>
            <Box sx={{ textAlign: "center" }}>
              <Typography component="h1" variant="h4" sx={{ fontWeight: 600, textWrap: "balance" }}>
                What do you want to build?
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                Describe it in your own words, and attach anything that explains it.
              </Typography>
            </Box>
            <PromptComposer
              prompt={prompt}
              onPromptChange={setPrompt}
              files={files}
              onFilesChange={setFiles}
              onSubmit={toDetails}
              inputRef={promptInput}
            />
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
                gap: 1.25,
                [PHONE]: { gridTemplateColumns: "1fr" },
              }}
            >
              {EXAMPLE_PROMPTS.map((example) => (
                <ExampleCard
                  key={example.title}
                  title={example.title}
                  prompt={example.prompt}
                  onPick={() => pickExample(example.prompt)}
                />
              ))}
            </Box>
          </Stack>
        </CenterPage>
      </BasePage>
    );
  }

  return (
    <BasePage>
      <CenterPage width={560}>
        <Stack spacing={2.5}>
          <Box>
            <Typography component="h1" variant="h5" sx={{ fontWeight: 600, mb: 0.5 }}>
              New project
            </Typography>
            {/* Labelled "Prompt:" so the user sees what is done with what they
                wrote: it is the agent's brief. One line, always: the textarea
                has no maxLength, and unclamped this echo could push Create
                project off the fold. The full text is on the title, and Back
                returns to the textarea still holding it. */}
            <Typography variant="body2" color="text.secondary" noWrap title={prompt}>
              <Box component="b" sx={{ color: "text.primary", fontWeight: 600 }}>
                Prompt:
              </Box>{" "}
              {prompt}
            </Typography>
          </Box>
          <TextField
            label="Project name"
            value={name}
            onChange={(e) => changeName(e.target.value)}
            error={Boolean(nameError)}
            helperText={nameError ?? "Suggested from your prompt. Change it if you like."}
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
              "Agentic Engineer creates this repository in your organization. Your specs and code live here, and it stays yours."
            }
            fullWidth
            slotProps={{
              htmlInput: { sx: { fontFamily: "monospace" } },
              input: {
                startAdornment: (
                  <Stack direction="row" spacing={0.75} sx={{ alignItems: "center", mr: 0.5, flexShrink: 0, color: "text.secondary" }}>
                    <GitHub size={16} />
                    <Typography variant="body2" color="text.secondary" sx={{ fontFamily: "monospace" }}>
                      {githubOrg ?? "<your-org>"}/
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
                    turn inputs and never enter the repo (ADR-0017). */}
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
            <CreateFailure
              error={createProject.error}
              onConnectGitHub={() => void navigate({ to: "/settings", search: { section: "github" } })}
              onRetry={create}
              retryDisabled={pending}
            />
          )}
          {uploadFailed && (
            <Alert severity="error">
              The project was created, but uploading the reference documents failed:{" "}
              {uploadReferences.error.message}
            </Alert>
          )}
          <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
            <Button
              variant="contained"
              onClick={create}
              disabled={!name || Boolean(nameError) || pending}
              loading={pending}
            >
              {/* Most specific first: the project exists and only the upload
                  failed (#383), the create is in flight (#561), or nothing
                  has happened yet. */}
              {createdName ? "Retry upload" : pending ? "Creating your project…" : "Create project"}
            </Button>
            {uploadFailed ? (
              // The explicit escape: go to the project without the documents,
              // releasing the kickoff the platform held for them.
              <Button variant="outlined" onClick={() => continueWithoutDocuments(createdName)} disabled={pending}>
                Continue without documents
              </Button>
            ) : (
              <Button variant="outlined" onClick={() => setStep("prompt")} disabled={pending || createdName !== null}>
                Back
              </Button>
            )}
          </Box>
        </Stack>
      </CenterPage>
    </BasePage>
  );
}

/**
 * A create the platform refused. Three refusals are states rather than failures
 * and say what to do: GitHub not connected goes to Settings' GitHub
 * section, AE Studio restarting offers Try again (the platform took the
 * half-made project away, so a retry is a clean create), and AE Studio
 * misconfigured names the administrator (no retry fixes it). An earlier delete
 * of the same project the platform could not finish says to wait. Anything else
 * is read out in the server's words.
 */
function CreateFailure({
  error,
  onConnectGitHub,
  onRetry,
  retryDisabled,
}: {
  error: unknown;
  onConnectGitHub: () => void;
  onRetry: () => void;
  retryDisabled: boolean;
}) {
  const code = error instanceof ApiRequestError ? error.code : undefined;
  if (code === "github_not_connected") {
    return (
      <Alert
        severity="warning"
        action={<Button onClick={onConnectGitHub}>Connect GitHub</Button>}
      >
        Connect GitHub to continue
      </Alert>
    );
  }
  if (code === "ae_studio_unavailable") {
    return (
      <Alert
        severity="info"
        action={
          <Button onClick={onRetry} disabled={retryDisabled}>
            Try again
          </Button>
        }
      >
        AE Studio is restarting — try again
      </Alert>
    );
  }
  if (code === "ae_studio_misconfigured") {
    return <Alert severity="error">AE Studio is misconfigured — contact your administrator</Alert>;
  }
  if (code === "project_delete_pending") {
    return (
      <Alert severity="info">
        An earlier delete of this project is still finishing. Try again in a minute.
      </Alert>
    );
  }
  return (
    <Alert severity="error">
      {error instanceof Error ? error.message : "Failed to create project"}
    </Alert>
  );
}
