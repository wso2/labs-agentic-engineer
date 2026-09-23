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

// The two enterprise fixtures @aep/prototype-model ships, parsed once, for the
// feature's tests. A fixture that stopped parsing fails here, loudly, rather
// than as a puzzling render further down.

import { parsePrototypeModel, type PrototypeModelV1 } from "@aep/prototype-model";
import expenseApprovalJson from "@aep/prototype-model/fixtures/expense-approval.json";
import integrationMonitorJson from "@aep/prototype-model/fixtures/integration-monitor.json";

function parsed(value: unknown, name: string): PrototypeModelV1 {
  const result = parsePrototypeModel(value);
  if (!result.ok) throw new Error(`fixture ${name} does not parse: ${JSON.stringify(result.issues)}`);
  return result.model;
}

export const expenseApproval = parsed(expenseApprovalJson, "expense-approval");
export const integrationMonitor = parsed(integrationMonitorJson, "integration-monitor");
