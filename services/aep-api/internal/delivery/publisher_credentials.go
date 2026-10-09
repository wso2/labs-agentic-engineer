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

// ErrPublisherCredentialsMissing is the refusal when the org has no
// ae-publisher-client reference: POST /build answers it before a tag is cut,
// and dispatch refuses the Job. Only the gitpat submit writes the reference,
// so retrying cannot create it — Temporal must not spend the re-dispatch
// budget on it.
var ErrPublisherCredentialsMissing = errors.New("publisher credentials missing")

// PublisherReconnectMessage is what a user is told when the org has no
// publisher credentials: reconnecting GitHub (the gitpat submit) is the one
// path that writes them.
const PublisherReconnectMessage = "Reconnect GitHub to set up this organization's build credentials"

// ErrTypePublisherCredentialsMissing is the Temporal ApplicationError TYPE the
// dispatch activity stamps. The workflow branches on the type because a
// sentinel does not survive the activity boundary.
const ErrTypePublisherCredentialsMissing = "PublisherCredentialsMissing"

// PublisherCredentialsMissingMessage is what the console shows for a run
// dispatch refused for the same reason: auto-kick and webhook adoption reach
// dispatch without the POST /build gate, so the text names the missing
// credential and the one way to create it.
const PublisherCredentialsMissingMessage = "This run cannot start because the organization's publisher credentials are not available to the coding agent. " + PublisherReconnectMessage + ", then retry."
