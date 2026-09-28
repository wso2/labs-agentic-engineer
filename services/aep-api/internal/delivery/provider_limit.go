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

// A MODEL PROVIDER LIMIT is the coding agent's other ending that is NOT a
// failure. The runner stops a run whose provider has refused its calls with
// HTTP 429 for longer than a wait — a `retry-after` of five minutes or more, or
// five minutes of 429 retries in total — and settles it with
// `code: provider_limit` (runners/remote-worker/src/lib/provider_limit.ts).
// Re-dispatching would only meet the same refusal until the plan resets, hours
// away, so the run settles BLOCKED under RunReasonModelProviderLimit, spends no
// re-dispatch budget, and a person starts it again: the same shape as
// ErrAgentQuotaExceeded, arriving after launch instead of at it.
//
// It lives here rather than in codingagent for the reason agent_quota.go does:
// the producer (the pod-truth watcher) and the consumer (the run supervisor)
// both import this package and neither imports the other.

// CycleReasonModelProviderLimit is the terminal reason the watcher writes on a
// cycle whose runner settled with the provider-limit code. The supervisor
// branches on it (CycleFacts.AgentReason); the host and the reset time ride
// the run's failure record, never this string, so nothing has to parse it.
const CycleReasonModelProviderLimit = "model_provider_limit"
