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

// ErrPublisherCredentialsMissing is the dispatch refusal when the org has no
// publisher SecretReference to mount. Retrying the Job create cannot stamp
// the ref — Temporal must not spend the re-dispatch budget on it.
var ErrPublisherCredentialsMissing = errors.New("publisher credentials missing")

// ErrTypePublisherCredentialsMissing is the Temporal ApplicationError TYPE the
// dispatch activity stamps. The workflow branches on the type because a
// sentinel does not survive the activity boundary.
const ErrTypePublisherCredentialsMissing = "PublisherCredentialsMissing"

// PublisherCredentialsMissingMessage is what the console shows. A project
// build is the path that stamps the SecretReference, but auto-kick and
// webhook adoption can reach dispatch without a successful stamp — so the
// text names the missing credential, not a single HTTP verb.
const PublisherCredentialsMissingMessage = "This run cannot start because the organization's publisher credentials are not available to the coding agent. Start a project build (secrets delivery must be on), then retry."
