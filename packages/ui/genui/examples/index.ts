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

import dependencyApproval from "./dependency-approval.json" with { type: "json" };
import buildAndDeploy from "./build-and-deploy.json" with { type: "json" };
import buildView from "./build-view.json" with { type: "json" };
import chatAgentAsks from "./chat-agent-asks.json" with { type: "json" };
import createCustomer from "./create-customer.json" with { type: "json" };
import customers from "./customers.json" with { type: "json" };
import deliveryStatus from "./delivery-status.json" with { type: "json" };

/**
 * Hand-written specs covering the catalog. They double as fixtures for the
 * tests and the demo page, and as few-shot examples when prompting a model.
 * Typed as unknown: a spec is untrusted until validateGenUiSpec() accepts it.
 */
export const exampleSpecs: Record<string, unknown> = {
  "Dependency approval": dependencyApproval,
  "Delivery status": deliveryStatus,
  "Build and deploy": buildAndDeploy,
  "Build view (console page)": buildView,
  "Create customer (form)": createCustomer,
  "Customers (list + form)": customers,
  "Chat: agent asks for details": chatAgentAsks,
};

export { componentExamples } from "./components.js";
