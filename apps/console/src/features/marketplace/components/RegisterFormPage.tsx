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

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  Chip,
  CircularProgress,
  Collapse,
  Divider,
  FormControlLabel,
  IconButton,
  PageContent,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { Plus, Trash2 } from "@wso2/oxygen-ui-icons-react";
import { Link, useNavigate } from "@tanstack/react-router";
import type { AskQuestionInput } from "@aep/agent-stream";
import { REGISTER_EXTERNAL_RESOURCE_COMMAND } from "@aep/contracts/commands";
import { useSession } from "../../../auth/SessionContext";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import type { components } from "../../../generated/aep-api";
import {
  rotateCurrentConversation,
} from "../../agent-chat/api/conversations";
import {
  addMessage,
  chatKeyFor,
  getMessages,
  replaceMessages,
  setPendingSeed,
  subscribe,
} from "../../agent-chat/chatStore";
import { pendingAnswerableQuestion } from "../../agent-chat/questionCards";
import {
  clearRegisterDraft,
  peekRegisterDraft,
  subscribeRegisterDraft,
} from "../../agent-chat/registerDraftStore";
import { AgentChatPanel } from "../../agent-chat/components/AgentChatPanel";
import { ChatQuestionForm } from "../../spec/components/ChatQuestionForm";
import {
  useExternalResources,
  useOrgEnvironments,
  useRegisterExternalResource,
  useUpdateExternalResource,
} from "../api/queries";
import { MARKETPLACE_CHAT_PROJECT } from "../constants";
import { isRegisteredExternal } from "../kind";
import { applyRegisterDraft } from "../lib/registerDraft";
import {
  envValueCellKey,
  validateRegisterForm,
} from "../lib/registerFormValidation";
import {
  rowsFromPointers,
  writesFromRows,
  ResourceDocsFields,
  type ResourceDocRow,
} from "./ResourceDocsFields";

type ConfigKeyDTO = components["schemas"]["ConfigKeyDTO"];
type EnvValueCellDTO = components["schemas"]["EnvValueCellDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

const KEEP_SECRET_HELPER = "Leave blank to keep the current value";

function slugFrom(prompt: string): string {
  const slug = prompt
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 32);
  return slug || "resource";
}

function envFieldLabel(environment: string, key: string): string {
  return `${environment} · ${key.trim() || "(unnamed key)"}`;
}

function cellStatus(
  cells: EnvValueCellDTO[] | null | undefined,
  environment: string,
  key: string,
): EnvValueCellDTO["status"] {
  const cell = (cells ?? []).find(
    (c) => c.environment === environment && c.key === key,
  );
  return cell?.status === "configured" ? "configured" : "unset";
}

function prefillFrom(record: ExternalResourceDTO): {
  name: string;
  description: string;
  consumptionInstructions: string;
  keys: ConfigKeyDTO[];
  values: Record<string, string>;
  docs: ResourceDocRow[];
} {
  const keys = (record.config ?? []).map((k) => ({
    key: k.key,
    description: k.description ?? "",
    secret: Boolean(k.secret),
  }));
  const secretKeys = new Set(keys.filter((k) => k.secret).map((k) => k.key));
  const values: Record<string, string> = {};
  for (const cell of record.envCells ?? []) {
    values[envValueCellKey(cell.environment, cell.key)] = secretKeys.has(cell.key)
      ? ""
      : (cell.value ?? "");
  }
  return {
    name: record.name,
    description: record.description ?? "",
    consumptionInstructions: record.consumptionInstructions ?? "",
    keys,
    values,
    docs: rowsFromPointers(record.resourceDocs ?? []),
  };
}

function fieldErr(message: string | undefined): { error: true; helperText: string } | Record<string, never> {
  return message ? { error: true, helperText: message } : {};
}

export function RegisterFormPage({
  prompt = "",
  name: editName,
}: {
  prompt?: string;
  name?: string;
}) {
  const isEdit = Boolean(editName);
  const promptTrimmed = prompt.trim();
  const seedRegister = !isEdit && Boolean(promptTrimmed);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { orgHandle } = useSession();
  const environments = useOrgEnvironments();
  const register = useRegisterExternalResource();
  const update = useUpdateExternalResource(editName ?? "");
  const resources = useExternalResources();
  const [chatOpen, setChatOpen] = useState(true);
  const seededRef = useRef(false);
  const [awaitingAgent, setAwaitingAgent] = useState(
    () => !isEdit && Boolean(promptTrimmed),
  );
  const [heldQuestions, setHeldQuestions] = useState<AskQuestionInput[] | null>(null);
  const [attemptedSubmit, setAttemptedSubmit] = useState(false);

  const record = (resources.data ?? []).find((r) => r.name === editName);
  const editing =
    isEdit && record != null && isRegisteredExternal(record) ? record : undefined;

  const [name, setName] = useState(
    isEdit ? (editName ?? "") : seedRegister ? "" : slugFrom(prompt),
  );
  const [description, setDescription] = useState(
    isEdit || seedRegister ? "" : prompt,
  );
  const [consumptionInstructions, setConsumptionInstructions] = useState("");
  const [keys, setKeys] = useState<ConfigKeyDTO[]>(
    isEdit || seedRegister
      ? []
      : [{ key: "API_KEY", description: "API secret", secret: true }],
  );
  const [values, setValues] = useState<Record<string, string>>({});
  const [docs, setDocs] = useState<ResourceDocRow[]>([]);
  const [prefilledName, setPrefilledName] = useState<string | null>(null);

  const chatKey = chatKeyFor(orgHandle ?? "default", MARKETPLACE_CHAT_PROJECT);
  const messages = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => getMessages(chatKey),
  );
  const pendingQuestion = pendingAnswerableQuestion(messages);
  const draft = useSyncExternalStore(
    useCallback((fn: () => void) => subscribeRegisterDraft(chatKey, fn), [chatKey]),
    () => peekRegisterDraft(chatKey),
  );
  const formRef = useRef({
    name,
    description,
    consumptionInstructions,
    keys,
    values,
    docs,
  });
  formRef.current = {
    name,
    description,
    consumptionInstructions,
    keys,
    values,
    docs,
  };

  useEffect(() => {
    if (!draft) return;
    const next = applyRegisterDraft(formRef.current, draft, {
      freezeName: isEdit,
      freezeKeys: isEdit,
    });
    setName(next.name);
    setDescription(next.description);
    setConsumptionInstructions(next.consumptionInstructions);
    setKeys(next.keys);
    setValues(next.values);
    setDocs(next.docs);
    setHeldQuestions(null);
    setAwaitingAgent(false);
  }, [draft, isEdit]);

  useEffect(() => {
    if (pendingQuestion) setHeldQuestions(null);
  }, [pendingQuestion]);

  useEffect(() => {
    if (seededRef.current || !seedRegister) return;
    seededRef.current = true;
    // Composer Start is a new register: demote the org-wide synthetic thread
    // first so prior walks cannot accumulate past the model prompt cap.
    replaceMessages(chatKey, []);
    clearRegisterDraft(chatKey);
    const instruction = `${REGISTER_EXTERNAL_RESOURCE_COMMAND} ${promptTrimmed}`;
    let cancelled = false;
    void (async () => {
      try {
        await rotateCurrentConversation(queryClient, MARKETPLACE_CHAT_PROJECT);
      } catch (err) {
        if (cancelled) return;
        addMessage(chatKey, {
          role: "error",
          content:
            err instanceof Error
              ? err.message
              : "Failed to start a new conversation.",
        });
        return;
      }
      if (cancelled) return;
      setPendingSeed(chatKey, instruction, true);
      void navigate({
        to: "/resources/register/form",
        replace: true,
        state: {},
      });
    })();
    return () => {
      cancelled = true;
      seededRef.current = false;
    };
  }, [chatKey, navigate, promptTrimmed, queryClient, seedRegister]);

  useEffect(() => {
    if (!editing || prefilledName === editing.name) return;
    const next = prefillFrom(editing);
    setName(next.name);
    setDescription(next.description);
    setConsumptionInstructions(next.consumptionInstructions);
    setKeys(next.keys);
    setValues(next.values);
    setDocs(next.docs);
    setPrefilledName(editing.name);
  }, [editing, prefilledName]);

  const envNames = (environments.data ?? []).map((e) => e.name);
  const envCells = editing?.envCells;
  const inFlight = messages.some((m) => m.role === "user" && m.status === "in_flight");
  const showQuestions = pendingQuestion?.questions ?? heldQuestions;
  const questionsSubmitting = Boolean(heldQuestions) && !pendingQuestion && !draft;
  const showFirstTurnSpinner =
    awaitingAgent &&
    !showQuestions &&
    !draft &&
    (inFlight || messages.length === 0);

  const errors = attemptedSubmit
    ? validateRegisterForm({
        name,
        description,
        consumptionInstructions,
        keys,
        values,
        envNames,
        isEdit,
        ...(envCells ? { envCells } : {}),
      })
    : null;

  const submitBlocked =
    environments.isLoading ||
    environments.isError ||
    envNames.length === 0 ||
    (isEdit && !editing);

  const submitError = isEdit
    ? update.error instanceof Error
      ? update.error.message
      : null
    : register.error instanceof Error
      ? register.error.message
      : null;
  const submitPending = isEdit ? update.isPending : register.isPending;

  const submit = () => {
    if (submitBlocked || submitPending) return;
    const invalid = validateRegisterForm({
      name,
      description,
      consumptionInstructions,
      keys,
      values,
      envNames,
      isEdit,
      ...(envCells ? { envCells } : {}),
    });
    if (invalid) {
      setAttemptedSubmit(true);
      return;
    }
    const resourceDocs = writesFromRows(docs);
    const body = {
      name: name.trim(),
      description: description.trim(),
      consumptionInstructions: consumptionInstructions.trim(),
      config: keys,
      envValues: keys.flatMap((cfg) =>
        envNames.map((environment) => ({
          environment,
          key: cfg.key,
          value: values[envValueCellKey(environment, cfg.key)] ?? "",
        })),
      ),
      ...(resourceDocs.length > 0 ? { resourceDocs } : {}),
    };
    const onSuccess = () => {
      void navigate({ to: "/resources" });
    };
    if (isEdit) {
      update.mutate(body, { onSuccess });
      return;
    }
    register.mutate(body, { onSuccess });
  };

  return (
    <PageContent
      fullWidth
      noPadding
      sx={{ height: "100%", display: "flex", flexDirection: "column" }}
    >
      <Box
        sx={{
          display: "flex",
          flexGrow: 1,
          width: "100%",
          minWidth: 0,
          height: "100%",
          minHeight: 0,
        }}
      >
        <Box
          sx={{
            flexGrow: 1,
            minWidth: 0,
            minHeight: 0,
            overflow: "auto",
            px: 3,
            py: 2,
          }}
        >
          <PageHeader
            title="Register External resource"
            subtitle="Environment values are form-only."
            backTo={{
              link: <Link to="/resources" />,
              label: "Back to Resources",
            }}
            {...(!chatOpen
              ? {
                  actions: (
                    <Button onClick={() => setChatOpen(true)}>Open agent chat</Button>
                  ),
                }
              : {})}
          />
          {environments.isError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              Failed to load environments
            </Alert>
          )}
          {submitError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {submitError}
            </Alert>
          )}
          {showQuestions?.length ? (
            <ChatQuestionForm
              org={orgHandle ?? "default"}
              projectName={MARKETPLACE_CHAT_PROJECT}
              questions={showQuestions}
              streaming={pendingQuestion?.streaming === true}
              submitting={questionsSubmitting}
              onSubmitted={() => setHeldQuestions(showQuestions)}
            />
          ) : showFirstTurnSpinner ? (
            <Box
              sx={{
                minHeight: 280,
                display: "flex",
                flexDirection: "column",
                gap: 2,
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <CircularProgress size={28} aria-label="The agent is working on this resource" />
              <Typography variant="body2" color="text.secondary">
                The agent is working on this resource
              </Typography>
            </Box>
          ) : (
          <Stack spacing={3} sx={{ maxWidth: 720 }} component="form" onSubmit={(e) => { e.preventDefault(); submit(); }}>
            <TextField
              label="Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              disabled={isEdit}
              {...fieldErr(errors?.name)}
            />
            <TextField
              label="Description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              required
              multiline
              minRows={2}
              {...fieldErr(errors?.description)}
            />

            <Box>
              <Stack direction="row" sx={{ justifyContent: "space-between", mb: 1 }}>
                <Typography variant="subtitle1">Config keys</Typography>
                {!isEdit && (
                  <Button
                    type="button"
                    size="small"
                    startIcon={<Plus size={14} />}
                    onClick={() =>
                      setKeys((prev) => [
                        ...prev,
                        { key: "", description: "", secret: false },
                      ])
                    }
                  >
                    Add key
                  </Button>
                )}
              </Stack>
              {errors?.configKeys ? (
                <Typography variant="caption" color="error" sx={{ display: "block", mb: 1 }}>
                  {errors.configKeys}
                </Typography>
              ) : null}
              <Stack spacing={2}>
                {keys.map((cfg, index) => (
                  <Stack
                    key={index}
                    direction="row"
                    spacing={1}
                    sx={{ alignItems: "center" }}
                  >
                    <TextField
                      label="Key"
                      value={cfg.key}
                      onChange={(e) =>
                        setKeys((prev) =>
                          prev.map((row, i) =>
                            i === index ? { ...row, key: e.target.value } : row,
                          ),
                        )
                      }
                      sx={{ flex: 1 }}
                      disabled={isEdit}
                      {...fieldErr(errors?.keys[index]?.key)}
                    />
                    <TextField
                      label="Description"
                      value={cfg.description ?? ""}
                      onChange={(e) =>
                        setKeys((prev) =>
                          prev.map((row, i) =>
                            i === index
                              ? { ...row, description: e.target.value }
                              : row,
                          ),
                        )
                      }
                      sx={{ flex: 2 }}
                      {...fieldErr(errors?.keys[index]?.description)}
                    />
                    <FormControlLabel
                      control={
                        <Checkbox
                          checked={Boolean(cfg.secret)}
                          disabled={isEdit}
                          onChange={(e) =>
                            setKeys((prev) =>
                              prev.map((row, i) =>
                                i === index
                                  ? { ...row, secret: e.target.checked }
                                  : row,
                              ),
                            )
                          }
                        />
                      }
                      label="Secret"
                    />
                    {!isEdit && (
                      <IconButton
                        aria-label="Remove key"
                        disabled={keys.length === 1}
                        onClick={() =>
                          setKeys((prev) => prev.filter((_, i) => i !== index))
                        }
                      >
                        <Trash2 size={16} />
                      </IconButton>
                    )}
                  </Stack>
                ))}
              </Stack>
            </Box>

            {keys.length > 0 ? (
            <Box>
              <Typography variant="subtitle1" gutterBottom>
                Environment values
              </Typography>
              {environments.isLoading ? (
                <Box sx={{ display: "flex", justifyContent: "center", py: 3 }}>
                  <CircularProgress aria-label="Loading environments" />
                </Box>
              ) : environments.isError ? null : envNames.length === 0 ? (
                <EmptyState
                  compact
                  bordered
                  title="No OpenChoreo Environments"
                  description="This organization has no OpenChoreo Environments, so environment values cannot be filled."
                />
              ) : (
                <Stack spacing={2} sx={{ pl: 2 }}>
                  {keys.map((cfg, index) => (
                    <Box key={cfg.key || `pending-${index}`}>
                      <Typography
                        variant="subtitle2"
                        color="text.secondary"
                        sx={{ mb: 1 }}
                      >
                        {cfg.key || "(unnamed key)"}
                      </Typography>
                      <Stack spacing={1.5}>
                        {envNames.map((environment) => {
                          const status = cellStatus(envCells, environment, cfg.key);
                          const valueErr =
                            errors?.values[envValueCellKey(environment, cfg.key)];
                          return (
                            <Stack
                              key={environment}
                              direction="row"
                              spacing={1}
                              sx={{ alignItems: "flex-start" }}
                            >
                              <TextField
                                label={envFieldLabel(environment, cfg.key)}
                                type={cfg.secret ? "password" : "text"}
                                value={values[envValueCellKey(environment, cfg.key)] ?? ""}
                                onChange={(e) =>
                                  setValues((prev) => ({
                                    ...prev,
                                    [envValueCellKey(environment, cfg.key)]: e.target.value,
                                  }))
                                }
                                sx={{ flex: 1 }}
                                error={Boolean(valueErr)}
                                helperText={
                                  valueErr ??
                                  (isEdit && cfg.secret ? KEEP_SECRET_HELPER : undefined)
                                }
                              />
                              {isEdit && (
                                <Chip
                                  size="small"
                                  variant="outlined"
                                  label={
                                    status === "configured" ? "Configured" : "Unset"
                                  }
                                  sx={{ mt: 1 }}
                                />
                              )}
                            </Stack>
                          );
                        })}
                      </Stack>
                    </Box>
                  ))}
                </Stack>
              )}
            </Box>
            ) : null}

            <Divider />

            <TextField
              label="Consumption instructions"
              value={consumptionInstructions}
              onChange={(e) => setConsumptionInstructions(e.target.value)}
              required
              multiline
              minRows={3}
              fullWidth
              {...fieldErr(errors?.consumptionInstructions)}
            />

            <ResourceDocsFields docs={docs} onChange={setDocs} />

            <Stack direction="row" spacing={2} sx={{ justifyContent: "flex-end" }}>
              <Button type="button" onClick={() => void navigate({ to: "/resources" })}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant="contained"
                disabled={submitBlocked || submitPending}
                loading={submitPending}
                onClick={(e) => {
                  e.preventDefault();
                  submit();
                }}
              >
                {isEdit ? "Save" : "Register"}
              </Button>
            </Stack>
          </Stack>
          )}
        </Box>
        {chatOpen ? (
          <Collapse
            in
            orientation="horizontal"
            unmountOnExit
            sx={{
              height: "100%",
              flexShrink: 0,
              alignSelf: "stretch",
              "& .MuiCollapse-wrapper, & .MuiCollapse-wrapperInner": {
                height: "100%",
              },
            }}
          >
            <AgentChatPanel
              org={orgHandle ?? "default"}
              projectName={MARKETPLACE_CHAT_PROJECT}
              specWorkspace={false}
              onClose={() => setChatOpen(false)}
            />
          </Collapse>
        ) : null}
      </Box>
    </PageContent>
  );
}
