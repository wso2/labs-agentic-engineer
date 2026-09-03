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

// How much of the requirements document is still unsettled (#575) — what the
// rail's Requirements section reports.
//
// Reuses the EDITOR's rules rather than reading the markdown itself. The
// document's flagged runs are located by `prdAffordances`, which is what draws
// the settle controls on those same lines; a second parser here would be a
// second definition of "what counts as an assumption", and the two would drift
// the first time either moved. The markdown is turned into the document shape
// that parser already walks, so there is exactly one set of rules.
//
// `deferred` is deliberately not counted. It is the user's own "stop asking me
// about this" — surfacing it as something to resolve would undo the answer they
// already gave.

import { markdownToNode } from "@aep/collab-doc";
import { prdAffordances } from "./prdLenses";
import { docBlocks } from "../collab/docBlocks";

export interface PrdUnsettled {
  /** Judgments the agent made, which the user may want to overturn. */
  assumptions: number;
  /** Gaps only the user can fill. */
  openQuestions: number;
}

const NONE: PrdUnsettled = { assumptions: 0, openQuestions: 0 };

/**
 * Count the unsettled entries in a requirements document.
 *
 * Best-effort: a document that will not parse yields nothing rather than
 * throwing. The rail is a read-out, and failing to render a workspace because
 * one document is malformed would cost the user far more than an uncounted
 * assumption.
 */
export function prdUnsettled(markdown: string | undefined): PrdUnsettled {
  if (!markdown?.trim()) return NONE;
  try {
    const flags = prdAffordances(docBlocks(markdownToNode(markdown))).flags;
    return {
      assumptions: flags.filter((f) => f.kind === "assumed").length,
      openQuestions: flags.filter((f) => f.kind === "question").length,
    };
  } catch {
    return NONE;
  }
}
