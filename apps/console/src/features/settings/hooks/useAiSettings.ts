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
  canAddSubscription,
  draftFrom,
  disconnectKeyPatch,
  draftProblem,
  refusedField,
  removesSubscription,
  type AiDraft,
  type AiField,
} from "../aiSettings";
import { useSaveAiSettings } from "../api/queries";

type ConfigProjection = components["schemas"]["ConfigProjection"];

/**
 * The AI agents card's unsaved draft, its one Save, and the key's disconnect.
 * The draft is compared with the server's state on every render, so a refetch
 * underneath an edit changes what Save would send, never what the reader typed.
 */
export function useAiSettings(config: ConfigProjection) {
  const saved = aiSettingsFrom(config);
  const [draft, setDraft] = useState<AiDraft>(() => draftFrom(saved));
  const [justSaved, setJustSaved] = useState(false);
  const mutation = useSaveAiSettings();
  // A second instance, so the disconnect dialog's pending and error states are
  // its own and not the Save button's.
  const disconnectMutation = useSaveAiSettings();

  const patch = aiSettingsPatch(saved, draft);
  const problem = draftProblem(saved, draft);

  const change = (next: Partial<AiDraft>) => {
    setJustSaved(false);
    setDraft((d) => ({ ...d, ...next }));
  };

  const save = () => {
    if (patch === null || problem !== undefined) return;
    mutation.mutate(patch, {
      onSuccess: (data) => {
        setDraft(draftFrom(aiSettingsFrom(data)));
        setJustSaved(true);
      },
    });
  };

  // Disconnecting takes the subscription with it (server-side), so the draft
  // drops its subscription and key fields; other unsaved edits survive.
  const disconnectKey = (onDone: () => void) => {
    disconnectMutation.mutate(disconnectKeyPatch(), {
      onSuccess: () => {
        setDraft((d) => ({ ...d, apiKey: "", billToSubscription: false, token: "" }));
        onDone();
      },
    });
  };

  const discard = () => {
    mutation.reset();
    setJustSaved(false);
    setDraft(draftFrom(saved));
  };

  return {
    saved,
    draft,
    change,
    save,
    discard,
    dirty: patch !== null,
    problem,
    removesSubscription: removesSubscription(saved, draft),
    canSave: patch !== null && problem === undefined && !mutation.isPending,
    saving: mutation.isPending,
    canAddSubscription: canAddSubscription(saved, draft),
    error: mutation.isError ? saveError(mutation.error) : undefined,
    justSaved,
    disconnectKey,
    disconnecting: disconnectMutation.isPending,
    disconnectError: disconnectMutation.isError
      ? disconnectMutation.error.message
      : undefined,
  };
}

function saveError(error: Error): { field: AiField; message: string } {
  const fields = error instanceof ApiRequestError ? error.fields : [];
  return { field: refusedField(fields), message: error.message };
}
