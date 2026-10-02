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

package organization

import (
	"maps"
	"time"
)

// OrgSecret names one of the six per-org secrets AE Studio and the build read
// from the vault. Its string is both the org_secrets.key of the secret's
// reference row and the entity segment of the reference name.
type OrgSecret string

const (
	OrgSecretGitHubPAT           OrgSecret = "github-pat"
	OrgSecretGitHubWebhookSecret OrgSecret = "github-webhook-secret"
	OrgSecretDefaultKey          OrgSecret = "default-key"
	OrgSecretCodingAgentKey      OrgSecret = "coding-agent-key"
	OrgSecretPublisherClient     OrgSecret = "ae-publisher-client"
	OrgSecretStudioClient        OrgSecret = "ae-studio-client"
)

// orgSecretKeys is the fixed set of data keys a write of each secret carries.
var orgSecretKeys = map[OrgSecret][]string{
	OrgSecretGitHubPAT:           {"token"},
	OrgSecretGitHubWebhookSecret: {"secret"},
	OrgSecretDefaultKey:          {"api-key"},
	OrgSecretCodingAgentKey:      {"api-key"},
	OrgSecretPublisherClient:     {"client_id", "client_secret"},
	OrgSecretStudioClient:        {"client_id", "client_secret"},
}

// OrgSecrets lists the six secrets in a fixed order.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func OrgSecrets() []OrgSecret {
	return []OrgSecret{
		OrgSecretGitHubPAT, OrgSecretGitHubWebhookSecret, OrgSecretDefaultKey,
		OrgSecretCodingAgentKey, OrgSecretPublisherClient, OrgSecretStudioClient,
	}
}

// Keys returns the data keys a write of s must carry, or nil for a name that
// is not one of the six.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (s OrgSecret) Keys() []string {
	keys, ok := orgSecretKeys[s]
	if !ok {
		return nil
	}
	return append([]string(nil), keys...)
}

// RefData returns what the reference stores for data. github-pat also stores
// the token as password, because the OpenChoreo build checkout reads password
// while tools and coding read token (one write, two properties of one value).
// Every other secret stores data unchanged. data itself is never modified.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (s OrgSecret) RefData(data map[string]string) map[string]string {
	out := maps.Clone(data)
	if s == OrgSecretGitHubPAT {
		out["password"] = data["token"]
	}
	return out
}

// OrgSecretRef is a secret's reference row: the secret is set, its value
// lives in the vault under the SecretReference called Name.
type OrgSecretRef struct {
	Secret    OrgSecret
	Name      string
	WrittenAt *time.Time
}
