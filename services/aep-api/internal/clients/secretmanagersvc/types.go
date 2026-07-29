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

// ErrSecretNotFound is returned when a secret does not exist.
var ErrSecretNotFound = secretsprovider.ErrSecretNotFound

// ErrNotManaged is returned when attempting to delete a secret not managed by this client.
var ErrNotManaged = secretsprovider.ErrNotManaged

// ErrMetadataNotFound is returned when secret metadata does not exist.
var ErrMetadataNotFound = secretsprovider.ErrMetadataNotFound

// ErrNotSupported is returned when an operation is not supported by the provider.
var ErrNotSupported = secretsprovider.ErrNotSupported

// SecretMetadata contains metadata for a secret.
type SecretMetadata = secretsprovider.SecretMetadata

// SecretInfo contains information about a secret without the actual values.
type SecretInfo = secretsprovider.SecretInfo

// StoreConfig holds configuration for secret store backends.
type StoreConfig = secretsprovider.StoreConfig

// OpenBaoConfig contains configuration for OpenBao/Vault.
type OpenBaoConfig = secretsprovider.OpenBaoConfig

// OpenBaoAuth contains authentication configuration for OpenBao.
type OpenBaoAuth = secretsprovider.OpenBaoAuth

// SecretLocation identifies where a secret lives in the KV hierarchy.
type SecretLocation = secretsprovider.SecretLocation

// ParseKVPath inverts KVPath for the six legal location shapes.
var ParseKVPath = secretsprovider.ParseKVPath
