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

// Package rolepermissions serves the POST /authz/role-permissions endpoint.
// It receives a map of AE roles to their AE permissions, delegates to
// authz.AuthZService.ModifyRolePermissions to translate and apply them as OC
// AuthzRole actions, and returns 200 OK.
//
// Triggers: modify-authz-role-permissions.
// Ports:    authz.AuthZService.
package rolepermissions
