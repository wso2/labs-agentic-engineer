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

import { createFileRoute } from "@tanstack/react-router";
import { ValidationLedger } from "../features/validation/components/ValidationLedger";

/**
 * The validation ledger — one row per version.
 *
 * `/validations`, plural, like `/builds` and `/deployments` beside it and the
 * API path it reads: the page is the ledger of every version's validations,
 * which is the thing that accumulates (lexicon, naming rule 3). The singular
 * `/validation` it replaced is not redirected — nothing outside the console
 * ever linked to it — and `?view=logs`, that page's old report/log toggle, is
 * dropped by validateSearch rather than honoured, because the two now sit on
 * one page.
 */
export const Route = createFileRoute("/projects/$projectName/validations/")({
  validateSearch: (): Record<string, never> => ({}),
  component: ValidationLedgerRoute,
});

function ValidationLedgerRoute() {
  const { projectName } = Route.useParams();
  return <ValidationLedger projectName={projectName} />;
}
