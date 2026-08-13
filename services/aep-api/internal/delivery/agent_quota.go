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

import "errors"

// ErrAgentQuotaExceeded is the dispatch refusal that is NOT a failure: the org
// has no agent-concurrency slot left, so nothing was launched and nothing is
// broken. The agent dispatcher returns it (mapped from the entitlement gate's
// HTTP 402); the run supervisor settles the run BLOCKED under
// RunReasonAgentQuotaBlocked instead of spending the cycle's re-dispatch budget
// on a wait that cannot help.
//
// It lives here rather than in codingagent because its producer (the coding
// executor) and its consumer (the run supervisor) both import this package and
// neither imports the other.
var ErrAgentQuotaExceeded = errors.New("agent concurrency quota exceeded")

// ErrTypeAgentQuotaBlocked is the Temporal ApplicationError TYPE the dispatch
// activity stamps on the quota refusal. The workflow branches on the type
// because a sentinel does not survive the activity boundary — Temporal
// round-trips errors as data, keeping only the message, the type and the
// details.
const ErrTypeAgentQuotaBlocked = "AgentQuotaBlocked"

// AgentQuotaBlockedMessage is what the console shows. It names the ACTION,
// because the only thing a user can do about a concurrency cap is wait or free
// a slot; the run row carries it as the terminal reason's explanation.
const AgentQuotaBlockedMessage = "This organization is already running the maximum number of agent runs allowed by its plan. " +
	"Wait for one to finish, or stop a running run, then start this one again."
