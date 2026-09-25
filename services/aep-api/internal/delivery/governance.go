// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package delivery

// The contract between the run slice, which calls governance as a stage, and
// the agentgovernance slice, which performs it. It lives in the domain root
// because two slices share it and a slice never imports its sibling.

// GovernAgentInput names one agent in one environment.
type GovernAgentInput struct {
	OrgID       string
	ProjectID   string
	Component   string
	Environment string
}

// GovernAgentOutcome distinguishes "governed" from "deliberately not governed".
//
// An environment with no AI gateway binding, or an org with no connected
// Anthropic key, is Skipped — not an error. The deploy then proceeds on the
// path it used before Agent Manager existed, which is what keeps this feature
// from breaking every environment that predates it.
type GovernAgentOutcome struct {
	Skipped bool
	Reason  string
	// ProxyURL is the address this agent's model traffic now leaves through —
	// its OWN proxy in Agent Manager, generated there and read back. Empty when
	// Skipped. Carried out so the build-time gate can name it on the ticket a
	// human attaches a guardrail from; the deploy path ignores it, because the
	// value it needs was written to the agent's secret alongside its key.
	ProxyURL string
}
