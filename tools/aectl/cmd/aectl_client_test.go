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

package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// aectl's own client is a machine client on the platform ThunderID, with a
// generated secret in a Secret of its own that no pod reads.
func TestAectlOpenChoreoClient_IsRegisteredOnThunder(t *testing.T) {
	i := slices.IndexFunc(thunderClients(), func(d idpClientDef) bool { return d.clientID == aectlOpenChoreoClientID })
	if i < 0 {
		t.Fatalf("%s is not among the clients aectl registers on ThunderID", aectlOpenChoreoClientID)
	}
	d := thunderClients()[i]
	if d.clientType != "confidential" || !d.noUserAttributes {
		t.Errorf("clientType = %q, noUserAttributes = %v; want a confidential machine client with no user attributes", d.clientType, d.noUserAttributes)
	}
	if d.vaultName != "aectl-openchoreo" || d.secretKey != "AECTL_OPENCHOREO_CLIENT_SECRET" {
		t.Errorf("vaultName = %q, secretKey = %q; want aectl-openchoreo / AECTL_OPENCHOREO_CLIENT_SECRET", d.vaultName, d.secretKey)
	}
	if d.secretNameOrDefault() != aectlClientSecretsName || aectlClientSecretsName == thunderSecretsName {
		t.Errorf("secret = %q; want its own Secret %q, not the shared %q", d.secretNameOrDefault(), aectlClientSecretsName, thunderSecretsName)
	}
	if !slices.Contains(generatedThunderClientNames, d.vaultName) {
		t.Errorf("%s is not a generated client secret", d.vaultName)
	}
}

// OpenChoreo grants secret:create/update to the client ID the platform chart
// names (aectl.openchoreoClient.clientId); aectl registers the client under
// its own constant. The two must stay the same ID.
func TestAectlOpenChoreoClient_MatchesTheChartsBinding(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deployments", "helm-charts", "platform", "values.yaml"))
	if err != nil {
		t.Fatalf("read platform chart values: %v", err)
	}
	var values struct {
		Aectl struct {
			OpenchoreoClient struct {
				ClientID string `yaml:"clientId"`
			} `yaml:"openchoreoClient"`
		} `yaml:"aectl"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("parse platform chart values: %v", err)
	}
	if got := values.Aectl.OpenchoreoClient.ClientID; got != aectlOpenChoreoClientID {
		t.Fatalf("chart aectl.openchoreoClient.clientId = %q, aectl registers %q", got, aectlOpenChoreoClientID)
	}
}
