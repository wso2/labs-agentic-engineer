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
// created on an ENVIRONMENT's identity provider: the Roles a project's design
// declares, and the Test users that exist so those roles' behaviour can be
// exercised.
//
// Two properties shape every type here, and both come from the objects being
// shared within one environment rather than project-scoped:
//
//   - **Neither roles nor test users are keyed by project — they are keyed by
//     (org, environment).** That pair IS the identity provider: every
//     environment of every org has its own (the environment tier, "T2"), and an
//     account minted on one is rejected by every other. Two projects in the same
//     org naming the same role mean the same role, and a person holding it holds
//     it everywhere IN THAT ENVIRONMENT. `TestUserRef` is the join that records
//     which projects USE which account; it is presentational, and nothing
//     branches on it.
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

// IdPRole is one role the platform created on an environment's identity
// provider.
//
// The key is (org, environment, name). The NAME carries the identity — it is
// what reaches an app as a `groups` claim, what OpenChoreo's authz bindings
// match on, and what a second project naming the same role means — and the pair
// in front of it says WHICH directory that name was created on. The Thunder
// group id is a detail that changes — a membership edit is a
// delete-and-recreate, because Thunder sets members only at create — so nothing
// may key on it.
type IdPRole struct {
	// OrgID and Environment name the identity provider this role lives on. They
	// are part of the key, not a filter: the same role name on two environments
	// is two groups, on two directories, that share nothing.
	OrgID       string `gorm:"column:org_id;primaryKey;type:text" json:"orgId"`
	Environment string `gorm:"column:environment;primaryKey;type:text" json:"environment"`
	// Name is the role name verbatim, as it appears in security.json and as the
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
	// directory, and deleting that project leaves it standing. CreatedByOrg is
	// kept even though OrgID now keys the row — an org's handle and the
	// OpenChoreo namespace its environments live in are the same string today,
	// and the provenance pair is read as a pair by the console.
	CreatedByOrg     string    `gorm:"column:created_by_org;type:text;index" json:"createdByOrg"`
	CreatedByProject string    `gorm:"column:created_by_project;type:text" json:"createdByProject"`
	CreatedAt        time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt        time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName pins the table name so a package rename cannot silently orphan the
// data.
func (IdPRole) TableName() string { return "idp_roles" }

// scope is the identity provider this row belongs to.
func (r IdPRole) scope() Scope { return Scope{OrgID: r.OrgID, Environment: r.Environment} }

// TestUser is one account the platform created so a role's behaviour can be
// exercised. The validation agent signs in as one to judge role-gated
// acceptance criteria.
//
// Keyed by (org, environment, username) for the same reason IdPRole is keyed by
// (org, environment, name): the username is what a sign-in presents and what a
// design names, and the pair says which identity provider that sign-in reaches.
type TestUser struct {
	OrgID       string `gorm:"column:org_id;primaryKey;type:text" json:"orgId"`
	Environment string `gorm:"column:environment;primaryKey;type:text" json:"environment"`
	Username    string `gorm:"column:username;primaryKey;type:text" json:"username"`
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

// scope is the identity provider this account lives on.
func (u TestUser) scope() Scope { return Scope{OrgID: u.OrgID, Environment: u.Environment} }

// TestUserRef records that a project's design references a test user. It is the
// ONLY project-scoped row in this package, and it is presentational: it drives
// the console's "referencing projects" column and lets the validation
// credential provider answer "which account serves role X for project P".
// Nothing about the directory object depends on it, and deleting the row
// deletes no account.
type TestUserRef struct {
	OrgID string `gorm:"column:org_id;primaryKey;type:text" json:"orgId"`
	// Environment completes the reference to the account: a username alone no
	// longer names one, because the same name may exist on two environments'
	// directories as two unrelated accounts.
	Environment string `gorm:"column:environment;primaryKey;type:text" json:"environment"`
	ProjectID   string `gorm:"column:project_id;primaryKey;type:text" json:"projectId"`
	Username    string `gorm:"column:username;primaryKey;type:text" json:"username"`
	// RoleName is denormalised from TestUser so the credential lookup is one
	// indexed query. The ensure rewrites it on every build, so it cannot drift.
	RoleName string `gorm:"column:role_name;type:text;not null;index" json:"roleName"`
	// ColdStart is a v1 LEFTOVER and is always false: v2 has no cold-start
	// account (signed-in operations and self-service enrolment replaced it),
	// and the ensure writes every ref without it. The field and its column stay
	// only so the stored row and the published API keep their shape; phase 5
	// drops both.
	ColdStart bool `gorm:"column:cold_start;not null;default:false" json:"coldStart"`
	// Supplied is true when the design named no test user for the role and the
	// platform generated the name. The console badges these.
	Supplied  bool      `gorm:"column:supplied;not null;default:false" json:"supplied"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

func (TestUserRef) TableName() string { return "test_user_refs" }

// IdPResourceServer is the platform's record of the OAuth resource server it
// created on an environment's identity provider for ONE project.
//
// It is the first row in this package that is keyed by project, and it is keyed
// that way because the object it records is project-OWNED rather than shared: a
// resource server, its resources and its actions are created by exactly one
// project, carry that project's name, and may therefore be converged — deleted
// when the design drops them and deleted outright when the project is deleted.
// Roles and groups keep the additive rules ADR-0022 set; these do not. See
// README.md, "project-owned converge, shared additive".
//
// Identifier is the absolute URI that reaches a generated app as the token's
// `aud`, and Thunder treats it as unique across the whole directory, so the
// uniqueness the row asserts is (org, environment, identifier) — one project per
// identifier on one directory.
type IdPResourceServer struct {
	// OrgID and Environment name the identity provider, exactly as on IdPRole:
	// the same project id on two environments is two resource servers, on two
	// directories, that share nothing.
	OrgID       string `gorm:"column:org_id;primaryKey;type:text;uniqueIndex:idx_idp_resource_servers_identifier,priority:1" json:"orgId"`
	Environment string `gorm:"column:environment;primaryKey;type:text;uniqueIndex:idx_idp_resource_servers_identifier,priority:2" json:"environment"`
	// ProjectID completes the key. One project has at most one resource server
	// on one directory.
	ProjectID string `gorm:"column:project_id;primaryKey;type:text" json:"projectId"`
	// Identifier is the resource server's absolute-URI identifier — the `aud` of
	// every token minted for this project's app, and the value the token
	// endpoint's `resource` parameter must match. Unique per directory, which is
	// what the second index above pins.
	Identifier string `gorm:"column:identifier;type:text;not null;uniqueIndex:idx_idp_resource_servers_identifier,priority:3" json:"identifier"`
	// DirectoryID is the directory's own id for the resource server. Unlike a
	// group id this one is stable — resource servers are updated in place — but
	// it is still a detail of the directory, so nothing keys on it. It exists
	// because a delete needs it: the cleanup path walks actions, resources and
	// then the resource server by id.
	DirectoryID string    `gorm:"column:directory_id;type:text;not null" json:"directoryId"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

func (IdPResourceServer) TableName() string { return "idp_resource_servers" }

// scope is the identity provider this resource server lives on.
func (r IdPResourceServer) scope() Scope { return Scope{OrgID: r.OrgID, Environment: r.Environment} }

// IdPRoleBinding is the platform's record of one project-owned role on an
// environment's identity provider, and of the group it was assigned to.
//
// A project role is named `<project>/<Role>` on the directory and, like the
// resource server above, is owned by the one project that declared it — so the
// ensure converges the set and a project delete removes it. That is the whole
// reason this row exists beside IdPRole: IdPRole records the SHARED org-wide
// role group, which is additive and is never deleted; this records the
// project's OWN role, which is not.
//
// The key carries GroupName because one role may be assigned to several groups.
// An EMPTY GroupName is meaningful and is the normal shape for a self-service
// role: the role exists and is recorded — so the delete path can find its
// DirectoryRoleID — but the build assigned it to nobody, because the
// registration flow assigns it per account instead.
type IdPRoleBinding struct {
	OrgID       string `gorm:"column:org_id;primaryKey;type:text" json:"orgId"`
	Environment string `gorm:"column:environment;primaryKey;type:text" json:"environment"`
	ProjectID   string `gorm:"column:project_id;primaryKey;type:text" json:"projectId"`
	// Role is the role name as the design declares it (`Approver`), NOT the
	// `<project>/Approver` handle the directory carries. The project half is
	// already in the key, and the console renders the design's name.
	Role string `gorm:"column:role;primaryKey;type:text" json:"role"`
	// GroupName is the directory group the role was assigned to, verbatim from
	// the design's `assignTo`. Empty means "recorded, assigned to nobody" — see
	// the type comment. It is part of the key so a role assigned to two groups
	// is two rows.
	GroupName string `gorm:"column:group_name;primaryKey;type:text" json:"groupName"`
	// DirectoryRoleID is the directory's id for the role. Denormalised onto every
	// binding of the same role, because a binding is what the delete path reads
	// and it must not need a second lookup to know what to remove.
	DirectoryRoleID string    `gorm:"column:directory_role_id;type:text;not null;index" json:"directoryRoleId"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt       time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

func (IdPRoleBinding) TableName() string { return "idp_role_bindings" }

// scope is the identity provider this role lives on — the accessor every row
// type in this file carries. The SQL store puts the pair in the WHERE clause
// rather than reading it off a row, so only the in-memory store model, which has
// to reproduce the table's key to be a faithful stand-in, calls this one.
//
//deadcode:keep uniform (org, environment) accessor; only the test store model calls it
func (b IdPRoleBinding) scope() Scope { return Scope{OrgID: b.OrgID, Environment: b.Environment} }

// key is the pair that identifies a binding WITHIN one (scope, project): the
// role and the group it is assigned to. ReplaceRoleBindings converges on it.
func (b IdPRoleBinding) key() [2]string { return [2]string{b.Role, b.GroupName} }
