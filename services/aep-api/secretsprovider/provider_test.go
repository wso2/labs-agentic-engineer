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

package secretsprovider_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/secretsprovider"
)

type stubProvider struct{}

func (stubProvider) NewClient(*secretsprovider.StoreConfig) (secretsprovider.SecretsClient, error) {
	return nil, nil
}

func (stubProvider) ValidateConfig(*secretsprovider.StoreConfig) error { return nil }

func (stubProvider) Capabilities() secretsprovider.StoreCapabilities {
	return secretsprovider.StoreCapabilityReadWrite
}

type stubRefManager struct{ stubProvider }

func (stubRefManager) ManagesSecretReferences() bool { return true }

type stubClient struct{}

func (stubClient) PushSecret(context.Context, secretsprovider.SecretLocation, []byte, *secretsprovider.SecretMetadata) (string, error) {
	return "", nil
}

func (stubClient) PatchSecret(context.Context, secretsprovider.SecretLocation, []byte, *secretsprovider.SecretMetadata) (string, error) {
	return "", nil
}

func (stubClient) DeleteSecret(context.Context, secretsprovider.SecretLocation, *secretsprovider.SecretMetadata) error {
	return nil
}

func (stubClient) GetSecret(context.Context, secretsprovider.SecretLocation) (*secretsprovider.SecretInfo, error) {
	return nil, nil
}

func (stubClient) GetSecretWithValue(context.Context, secretsprovider.SecretLocation) ([]byte, error) {
	return nil, nil
}

func (stubClient) Close(context.Context) error { return nil }

func TestProviderInterfaces_CompileAssert(t *testing.T) {
	var _ secretsprovider.Provider = stubProvider{}
	var _ secretsprovider.SecretReferenceManager = stubRefManager{}
	var _ secretsprovider.SecretsClient = stubClient{}
}

func TestSecretLocation_KVPath(t *testing.T) {
	loc := secretsprovider.SecretLocation{
		OrgName:    "acme",
		EntityName: "anthropic",
		SecretKey:  "api-key",
	}
	got, err := loc.KVPath()
	if err != nil {
		t.Fatalf("KVPath: %v", err)
	}
	if got != "acme/anthropic/api-key" {
		t.Fatalf("got %q, want acme/anthropic/api-key", got)
	}
}

func TestSecretLocation_SecretRefName(t *testing.T) {
	loc := secretsprovider.SecretLocation{EntityName: "anthropic"}
	if got := loc.SecretRefName(); got != "anthropic-secrets" {
		t.Fatalf("got %q, want anthropic-secrets", got)
	}
}

func TestParseKVPath_RoundTrip(t *testing.T) {
	loc := secretsprovider.SecretLocation{
		OrgName:     "acme",
		ProjectName: "weather",
		EntityName:  "openweather",
	}
	path, err := loc.KVPath()
	if err != nil {
		t.Fatalf("KVPath: %v", err)
	}
	parsed, err := secretsprovider.ParseKVPath(path)
	if err != nil {
		t.Fatalf("ParseKVPath: %v", err)
	}
	if parsed != loc {
		t.Fatalf("round-trip: got %+v, want %+v", parsed, loc)
	}
}
