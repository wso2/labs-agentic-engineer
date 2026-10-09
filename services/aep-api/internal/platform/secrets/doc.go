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

// Package secrets is the kernel module that owns every secret BACKEND.
//
// The org secrets live only in the vault (OpenBao, delivered to pods through
// ExternalSecrets); Postgres holds their reference names, never a value. The
// one sealed Postgres column, test_users.password_sealed, is sealed by
// ColumnCipher. This module gives the backends ONE home and ONE arch fence: the
// OpenBao/Vault SDK may be imported from here and nowhere else
// (TestImportFences), so no business domain can reach a backend directly.
//
// It exposes a FEW purpose-specific ports, never one god port. The
// user-facing "connect my GitHub / model key / resource secret" capabilities
// are NOT here — they stay in their business domains (organization,
// dependencies) and call these ports.
package secrets
