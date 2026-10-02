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

// Package getaestudio reports the caller's org's AE Studio.
//
// Trigger: GET /ae-studio (get-ae-studio), behind the deny-by-default tenant gate.
// In→out:  the gate-bound org → the AeStudio state and, once ready, its URLs.
// Ports:   organization.AEStudioStatusReader.
// Invariant: the org comes only from the gate-bound context, never the request,
//
//	and no bound org (the gate in LOG mode) answers 401; an install without
//	AE Studio configuration answers failed, not 500.
package getaestudio
