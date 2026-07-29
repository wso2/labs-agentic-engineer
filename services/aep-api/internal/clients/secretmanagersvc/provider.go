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

package secretmanagersvc

import "github.com/wso2/aep/aep-api/secretsprovider"

// StoreCapabilities defines what operations a provider supports.
type StoreCapabilities = secretsprovider.StoreCapabilities

const (
	// StoreCapabilityReadOnly indicates the provider can only read secrets.
	StoreCapabilityReadOnly = secretsprovider.StoreCapabilityReadOnly
	// StoreCapabilityWriteOnly indicates the provider can only write secrets.
	StoreCapabilityWriteOnly = secretsprovider.StoreCapabilityWriteOnly
	// StoreCapabilityReadWrite indicates the provider can read and write secrets.
	StoreCapabilityReadWrite = secretsprovider.StoreCapabilityReadWrite
)

// Provider creates SecretsClient instances for a specific backend.
type Provider = secretsprovider.Provider

// SecretReferenceManager signals that a provider manages SecretReference CRDs internally.
type SecretReferenceManager = secretsprovider.SecretReferenceManager

// SecretsClient performs secret operations on a backend.
type SecretsClient = secretsprovider.SecretsClient
