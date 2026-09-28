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

package auth

import "context"

// UnknownActor is what ActorFromContext answers when no verified subject is
// available. Named because callers that must not INVENT an identity — as
// opposed to those merely attributing an audit line — have to be able to
// recognise it.
const UnknownActor = "unknown"

// ActorFromContext returns the requesting user's identifier (the verified
// JWT subject) for audit attribution. Falls back to UnknownActor when the JWT
// middleware didn't populate claims (e.g. an unauthenticated path).
func ActorFromContext(ctx context.Context) string {
	if c := ClaimsFromContext(ctx); c != nil && c.Subject != "" {
		return c.Subject
	}
	return UnknownActor
}
