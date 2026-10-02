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

package repo

import "context"

// Credential mints the token for one remote git op. The engine hands it to git
// through the askpass shim only: never argv, never git config.
type Credential interface {
	Token(ctx context.Context) (string, error)
}

// StaticToken is the Credential over the mounted gitpat. The container exits
// on a secret revision mismatch, so the value never changes within a process
// and one config read at boot is the single source.
type StaticToken string

// Token returns the fixed token.
//
//deadcode:keep wired in Task 2.7
func (t StaticToken) Token(context.Context) (string, error) { return string(t), nil }
