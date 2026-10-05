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
// Secret storage is spread across four genuinely different backends — OpenBao
// (sealed git tokens), the SM-API (runner mirrors), Kubernetes ExternalSecrets
// (pods), and Postgres (the publisher secret). This module gives them ONE home
// and ONE arch fence: the OpenBao/Vault SDK may be imported from here and nowhere
// else (TestImportFences), so no business domain can reach a backend directly.
//
// It exposes a FEW purpose-specific ports, never one god port: the four backends
// serve different purposes, and one interface over them would be a false common
// core. The user-facing "connect my GitHub / Anthropic / resource secret"
// capabilities are NOT here — they stay in their business domains
// (organization, dependencies) and call these ports.
//
// Migration note: this was internal/credentials. The other three backends are
// routed through it in P3, not P0.
package secrets
