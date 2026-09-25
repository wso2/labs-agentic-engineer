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

import { validateGenUiSpec, type GenUiSpec } from "@aep/ui-genui";
import { exampleSpecs } from "@aep/ui-genui/examples";

export interface ChatMessage {
  id: string;
  from: "agent" | "user";
  text: string;
  /** A card the agent sent, checked before it was posted. */
  spec?: GenUiSpec;
}

let nextId = 0;
const id = () => `m${++nextId}`;

/** The scripted agent's opening turn: a question, as a form card. */
export function openingMessage(): ChatMessage {
  return {
    id: id(),
    from: "agent",
    text: "To set up the payments dependency I need a few details.",
    // A fresh copy per conversation: each card keeps its own state.
    spec: structuredClone(exampleSpecs["Chat: agent asks for details"]) as GenUiSpec,
  };
}

export function userMessage(text: string): ChatMessage {
  return { id: id(), from: "user", text };
}

/**
 * The agent's answer to a valid reply. A real agent would write this spec
 * itself; here it is built from the answers. Either way it goes through the
 * same check before it is posted, as the agent's show_ui tool would do.
 */
export function confirmationMessage(facts: Array<{ label: string; value: string }>): ChatMessage {
  const spec = {
    root: "card",
    elements: {
      card: {
        type: "Card",
        props: {
          title: "Payments dependency configured",
          subtitle: "Saved to the development environment.",
        },
        children: ["status", "facts", "open"],
      },
      status: {
        type: "StatusChip",
        props: { label: "Configured", tone: "success" },
        children: [],
      },
      facts: {
        type: "KeyValueList",
        props: { items: facts.filter((f) => f.value !== "—") },
        children: [],
      },
      open: {
        type: "Button",
        props: { label: "Open deployments", variant: "secondary" },
        on: { press: { action: "openDeployments", params: {} } },
        children: [],
      },
    },
  };
  const checked = validateGenUiSpec(spec);
  return checked.ok
    ? { id: id(), from: "agent", text: "Done. Here is what I set up:", spec: checked.spec }
    : { id: id(), from: "agent", text: `My card was rejected: ${checked.issues.join("; ")}` };
}
