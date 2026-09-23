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

/**
 * The review shell with a live in-memory queue — what the page gives it,
 * without the turn behind Send all. Shared by the shell's jsdom and browser
 * suites so both drive the same component the same way.
 */

import { useState } from "react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { PrototypeModelV1 } from "@aep/prototype-model";
import type { PrototypeAnnotation } from "../model/annotations";
import type { PrototypeViewRequest } from "../model/viewState";
import { PrototypeShell } from "../components/PrototypeShell";

export interface ShellHarnessProps {
  model: PrototypeModelV1;
  request?: PrototypeViewRequest;
  busy?: boolean;
  sending?: boolean;
  error?: string | null;
  onSendAll?: () => void;
  onRequestChange?: (next: PrototypeViewRequest) => void;
}

export function ShellHarness({ model, request = {}, busy = false, sending = false, error = null, onSendAll = () => {}, onRequestChange = () => {} }: ShellHarnessProps) {
  const [annotations, setAnnotations] = useState<PrototypeAnnotation[]>([]);
  return (
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <PrototypeShell
        model={model}
        request={request}
        onRequestChange={onRequestChange}
        backLink={<a href="/spec" />}
        feedback={{
          annotations,
          onAdd: (a) => setAnnotations((q) => [...q, { ...a, id: `req-${q.length + 1}` }]),
          onRemove: (id) => setAnnotations((q) => q.filter((a) => a.id !== id)),
          onSendAll,
          sending,
          busy,
          error,
        }}
      />
    </OxygenUIThemeProvider>
  );
}
