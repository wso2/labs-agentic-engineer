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

package identity

import "context"

// Directory is the slice of the Platform IdP admin client this domain uses.
// It is a narrowing of thundersvc.Client, mapped at the composition root, so
// the identity domain names no client package.
//
// Its shape is dictated by one IdP constraint: membership can only be set when
// a group is CREATED. That is why CreateGroup takes members, and why AddMembers
// exists at all rather than a plain add-one-member call.
type Directory interface {
	ListGroups(ctx context.Context) ([]DirectoryGroup, error)
	FindGroupByName(ctx context.Context, name string) (*DirectoryGroup, bool, error)
	// GroupMembers returns the accounts in a group. The catalog reads it for a
	// member count; the ensure does NOT — its add path reads membership itself,
	// inside the lock that makes the read-modify-write safe.
	GroupMembers(ctx context.Context, groupID string) ([]string, error)
	CreateGroup(ctx context.Context, name, description string, memberIDs []string) (DirectoryGroup, error)
	// AddMembers adds members to an existing group, keeping the ones already
	// there. The implementation reads and writes under one lock, because the
	// only membership write the IdP offers is a delete-and-recreate of the whole
	// group. Destructive by construction: the ensure calls it ONLY for a group
	// it owns. Returns the group's new identity.
	AddMembers(ctx context.Context, group DirectoryGroup, memberIDs []string) (DirectoryGroup, error)

	FindUserByUsername(ctx context.Context, username string) (*DirectoryAccount, bool, error)
	CreateUser(ctx context.Context, username, email, password string) (DirectoryAccount, error)
	SetUserPassword(ctx context.Context, userID, password string) error
	DeleteUser(ctx context.Context, userID string) error
}

// DirectoryGroup is one group on the IdP.
type DirectoryGroup struct {
	ID          string
	Name        string
	Description string
	OUID        string
}

// DirectoryAccount is one account on the IdP. No password: the IdP does not
// return one.
type DirectoryAccount struct {
	ID       string
	Username string
	Email    string
}

// DesignReader reads the design bundle at a spec tag — keys relative to
// specs/design/, so the roles document is `roles.json`.
type DesignReader interface {
	GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
}
