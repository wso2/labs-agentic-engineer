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

// Package identity is the platform's record of the SHARED directory objects it
// created on the Platform IdP: the Roles a project's design declares, and the
// Test users that exist so those roles' behaviour can be exercised.
//
// Two properties shape every type here, and both come from the objects being
// shared rather than project-scoped:
//
//   - **Neither roles nor test users are keyed by project.** Their scope is the
//     IdP's scope — cluster-wide while one IdP serves the cluster. Two projects
//     naming the same role mean the same role, and a person holding it holds it
//     everywhere. `TestUserRef` is the join that records which projects USE
//     which account; it is presentational, and nothing branches on it.
//
//   - **A row here IS the platform's ownership marker.** Thunder rejects custom
//     attributes (400 USR-1019), so the platform cannot stamp "I made this" on
//     the directory object itself. The presence of an `idp_roles` row therefore
//     means "this platform created this role", and the presence of a
//     `test_users` row means "this platform owns this account". Those two facts
//     are what stop the ensure from enrolling a member into the `Administrators`
//     group somebody else made, and from resetting a real person's password
//     because a design named their username.
package identity

import "time"

// IdPRole is one role the platform created on the identity provider.
//
// The name is the primary key, not a surrogate id, because the NAME is the
// identity: it is what reaches an app as a `groups` claim, what OpenChoreo's
// authz bindings match on, and what a second project naming the same role
// means. The Thunder group id is a detail that changes — a membership edit is a
// delete-and-recreate, because Thunder sets members only at create — so nothing
// may key on it.
type IdPRole struct {
	// Name is the role name verbatim, as it appears in roles.json and as the
	// directory group name.
	Name string `gorm:"column:name;primaryKey;type:text" json:"name"`
	// ThunderGroupID is the current directory group id. It CHANGES whenever the
	// group's membership is edited; treat it as a cache, never as identity.
	ThunderGroupID string `gorm:"column:thunder_group_id;type:text;not null" json:"thunderGroupId"`
	// Description is what the platform seeded the group with at create. It is
	// never used to update an existing group — a shared role may have been
	// described by somebody else first.
	Description string `gorm:"column:description;type:text" json:"description"`
	// CreatedByOrg / CreatedByProject record who first declared the role. They
	// are provenance for the console, not a scope: the role belongs to the
	// directory, and deleting that project leaves it standing.
	CreatedByOrg     string    `gorm:"column:created_by_org;type:text;index" json:"createdByOrg"`
	CreatedByProject string    `gorm:"column:created_by_project;type:text" json:"createdByProject"`
	CreatedAt        time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt        time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName pins the table name so a package rename cannot silently orphan the
// data.
func (IdPRole) TableName() string { return "idp_roles" }

// TestUser is one account the platform created so a role's behaviour can be
// exercised. The validation agent signs in as one to judge role-gated
// acceptance criteria.
//
// Keyed by username for the same reason IdPRole is keyed by name: the username
// is what a sign-in presents and what a design names.
type TestUser struct {
	Username string `gorm:"column:username;primaryKey;type:text" json:"username"`
	// ThunderUserID is the directory account id. Unlike a group id this one is
	// stable — a password rotate is an update, not a recreate.
	ThunderUserID string `gorm:"column:thunder_user_id;type:text;not null" json:"thunderUserId"`
	// RoleName is the role this account holds. It matches an IdPRole.Name.
	RoleName string `gorm:"column:role_name;type:text;not null;index" json:"roleName"`
	// PasswordSealed is the generated password under AES-256-GCM
	// (credential-encryption-key), the same framing as publisher_client_secret.
	//
	// It exists because Thunder will not give a password back: `GET /users/{id}`
	// returns no password field at all. Without a sealed copy a credential could
	// be issued exactly once and never served again, so the validation runner
	// asking twice would invalidate its own first answer.
	//
	// `json:"-"` keeps it off every wire shape; the reveal endpoint opens it
	// deliberately and separately.
	PasswordSealed string    `gorm:"column:password_sealed;type:text" json:"-"`
	Email          string    `gorm:"column:email;type:text" json:"email"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"createdAt"`
	// RotatedAt is when the password was last replaced, nil when never.
	RotatedAt *time.Time `gorm:"column:rotated_at" json:"rotatedAt,omitempty"`
}

func (TestUser) TableName() string { return "test_users" }

// TestUserRef records that a project's design references a test user. It is the
// ONLY project-scoped row in this package, and it is presentational: it drives
// the console's "referencing projects" column and lets the validation
// credential provider answer "which account serves role X for project P".
// Nothing about the directory object depends on it, and deleting the row
// deletes no account.
type TestUserRef struct {
	OrgID     string `gorm:"column:org_id;primaryKey;type:text" json:"orgId"`
	ProjectID string `gorm:"column:project_id;primaryKey;type:text" json:"projectId"`
	Username  string `gorm:"column:username;primaryKey;type:text" json:"username"`
	// RoleName is denormalised from TestUser so the credential lookup is one
	// indexed query. The ensure rewrites it on every build, so it cannot drift.
	RoleName string `gorm:"column:role_name;type:text;not null;index" json:"roleName"`
	// ColdStart marks the account holding this project's cold-start role — the
	// one the credential provider serves when a caller asks without naming a
	// role.
	ColdStart bool `gorm:"column:cold_start;not null;default:false" json:"coldStart"`
	// Supplied is true when the design named no test user for the role and the
	// platform generated the name. The console badges these.
	Supplied  bool      `gorm:"column:supplied;not null;default:false" json:"supplied"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

func (TestUserRef) TableName() string { return "test_user_refs" }
