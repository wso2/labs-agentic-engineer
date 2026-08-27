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

// Package rolespanel serves the console's Security panel: the shared role
// catalog joined against the platform's own record, this project's test
// accounts, and the three actions offered on one of those accounts.
//
// Triggers: get-project-roles, reveal-test-user-password,
// rotate-test-user-password, delete-test-user.
// Ports:    identity.PanelService (the fenced domain service).
//
// The org is taken from the tenant fence (tenant.BoundOrgFromContext) and never
// from a parameter — there is no {orgHandle} anywhere in the contract — and the
// project comes from the path. Both are handed to the domain service, which
// applies the org+project and ownership fences; this package decides only how
// the domain's refusals reach the wire.
package rolespanel
