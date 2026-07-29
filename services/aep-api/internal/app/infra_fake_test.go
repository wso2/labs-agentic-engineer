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

package app

import (
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// Fake returns a zero-I/O Infra for assembly tests. The DB is a non-nil but
// UNCONNECTED *gorm.DB: the graph's constructors only store it (and some assert
// it is non-nil, e.g. JobWatcher) — nothing queries it at assembly time, so no
// connection is opened. The workspace is nil for the same reason (constructors
// store the pointer; the trash hooks / reaper only deref it when invoked). The
// minter (no-app mode) and credential store (cipher-only over the nil DB) are
// both pure to construct. app.Assemble(cfg, Fake(), Seam{}) thus builds the same real
// handler + watchers as production without touching the network, clock, or disk.
func Fake() Infra {
	minter, _ := secrets.NewAppTokenMinter(nil)               // no-app mode, no I/O
	credStore, _ := secrets.NewDBStore(nil, make([]byte, 32)) // AES cipher only, nil DB
	return Infra{
		DB:              &gorm.DB{},
		CredentialStore: credStore,
		Minter:          minter,
		AppClientSecret: "",
		K8sClient:       nil,
		Workspace:       nil,
	}
}
