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
import type { components } from "../../../generated/aep-api";
import { ApiRequestError } from "../../../api/errors";
import {
  aiSettingsFrom,
  aiSettingsPatch,
  connectionView,
  disconnectPatch,
  draftFrom,
  draftProblem,
  keyRequired,
  refusedField,
  removesSubscription,
  runtimeMovedByFormat,
  subscriptionOffered,
  switchFormat,
  testBody,
  type AiDraft,
  type AiField,
  type LLMCheck,
  type LLMFormat,
} from "../aiSettings";
import { useSaveAiSettings, useTestConnection } from "../api/queries";

type ConfigProjection = components["schemas"]["ConfigProjection"];

/** The draft fields that name the connection; changing one voids a test result. */
const CONNECTION_FIELDS: (keyof AiDraft)[] = ["kind", "baseURL", "model", "apiKey"];

/**
 * The AI agents card's unsaved draft, its Test connection, its one Save, and
 * the connection's disconnect. The draft is compared with the server's state
 * on every render, so a refetch underneath an edit changes what Save would
 * send, never what the reader typed.
 *
 * `check` is the latest probe of the draft's connection: a test result, or the
 * `llmCheck` a probing save returned. It is dropped as soon as the reader
 * edits the connection, since it then describes a connection the draft no
 * longer names.
 */
export function useAiSettings(config: ConfigProjection) {
  const saved = aiSettingsFrom(config);
  const [draft, setDraft] = useState<AiDraft>(() => draftFrom(saved));
  const [check, setCheck] = useState<LLMCheck | null>(null);
  const [justSaved, setJustSaved] = useState(false);
  const mutation = useSaveAiSettings();
  const test = useTestConnection();
  // A second instance, so the disconnect dialog's pending and error states are
  // its own and not the Save button's.
  const disconnectMutation = useSaveAiSettings();

  const offered = subscriptionOffered(saved, draft, check);
  const patch = aiSettingsPatch(saved, draft, offered);
  const problem = draftProblem(saved, draft, offered);

  const voidProbe = () => {
    setCheck(null);
    test.reset();
    mutation.reset();
  };

  const change = (next: Partial<AiDraft>) => {
    setJustSaved(false);
    if (CONNECTION_FIELDS.some((f) => f in next)) voidProbe();
    setDraft((d) => ({ ...d, ...next }));
  };

  const chooseFormat = (kind: LLMFormat) => {
    setJustSaved(false);
    voidProbe();
    setDraft((d) => switchFormat(d, saved.formats, kind));
  };

  const testConnection = () => {
    mutation.reset();
    test.mutate(testBody(draft), { onSuccess: setCheck });
  };

  const save = () => {
    if (patch === null || problem !== undefined) return;
    test.reset();
    mutation.mutate(patch, {
      onSuccess: (data) => {
        setDraft(draftFrom(aiSettingsFrom(data)));
        setCheck(data.llmCheck ?? null);
        setJustSaved(true);
      },
    });
  };

  // Disconnecting takes the subscription with it (server-side), so the draft
  // drops its key and token; other unsaved edits survive.
  const disconnect = (onDone: () => void) => {
    disconnectMutation.mutate(disconnectPatch(), {
      onSuccess: () => {
        setCheck(null);
        setDraft((d) => ({ ...d, apiKey: "", token: "", removeToken: false }));
        onDone();
      },
    });
  };

  const discard = () => {
    voidProbe();
    setJustSaved(false);
    setDraft(draftFrom(saved));
  };

  const testError = test.isError ? probeError(test.error) : undefined;
  const saveErr = mutation.isError ? probeError(mutation.error) : undefined;

  return {
    saved,
    draft,
    change,
    chooseFormat,
    save,
    discard,
    dirty: patch !== null,
    problem,
    keyRequired: keyRequired(saved, draft),
    view: connectionView(saved, draft, check),
    check,
    subscriptionOffered: offered,
    runtimeMoved: runtimeMovedByFormat(saved, draft),
    removesSubscription: removesSubscription(saved, draft, offered),
    canSave: patch !== null && problem === undefined && !mutation.isPending && !test.isPending,
    saving: mutation.isPending,
    testConnection,
    testing: test.isPending,
    // A save and a test refuse with the same codes; whichever ran last speaks.
    error: saveErr ?? testError,
    justSaved,
    disconnect,
    disconnecting: disconnectMutation.isPending,
    disconnectError: disconnectMutation.isError
      ? disconnectMutation.error.message
      : undefined,
  };
}

function probeError(error: Error): { field: AiField; message: string } {
  if (!(error instanceof ApiRequestError)) {
    return { field: "card", message: error.message };
  }
  return { field: refusedField(error.fields, error.code), message: error.message };
}
