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

// Package skills embeds the platform-bundled skill library shipped in the
// aep-api binary — the shipping vehicle; each org's `org-skills` repo is the
// live store the library is seeded + content-reconciled into
// (docs/design/skills-unified-library-migration.md, skills-repo-storage.md).
//
// embedded/ is a `go generate` VENDOR of the repo-root skills/ directory —
// the single authored source of truth, which go:embed cannot reach across
// the module boundary. Keep in sync via `make vendor-skills`; CI runs
// `make vendor-skills-check`. Each skill is embedded/<name>/SKILL.md
// (+ optional references/*.md); its kind lives in the SKILL.md frontmatter
// (`metadata.aep.kind`: platform | org, absent → org).
package skills

import "embed"

//go:generate sh -c "rm -rf embedded && cp -R ../../../skills embedded"
//go:embed embedded
var LibraryFS embed.FS
