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

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// Store is the persistence surface for the platform's record of the shared
// directory objects it created.
//
// Note what is NOT org-scoped here, deliberately, and against this codebase's
// usual rule that every accessor carries an org filter: `idp_roles` and
// `test_users` are directory objects, and the directory is shared at the IdP's
// scope. Fencing them by org would model a per-org directory that does not
// exist, and would make one org's ensure create a duplicate of a role another
// org already made — the exact near-duplicate the design-time catalog exists to
// prevent. `test_user_refs`, which IS project-scoped, carries the fence.
type Store interface {
	// -- roles ----------------------------------------------------------

	// GetRole returns the platform's record of a role by name, or nil when the
	// platform did not create it. A nil result is the ownership answer: the
	// role may well exist on the directory, but it is not ours to modify.
	GetRole(ctx context.Context, name string) (*IdPRole, error)
	// ListRoles returns every role the platform created, name-ordered.
	ListRoles(ctx context.Context) ([]IdPRole, error)
	// UpsertRole records a role the platform created, or refreshes the cached
	// Thunder group id after a membership edit recreated the group.
	UpsertRole(ctx context.Context, role IdPRole) error

	// -- test users ------------------------------------------------------

	// GetTestUser returns the platform's record of an account, or nil when the
	// platform does not own it. As with GetRole, nil means hands off.
	GetTestUser(ctx context.Context, username string) (*TestUser, error)
	// UpsertTestUser records an account the platform created. The password is
	// sealed on the way in.
	UpsertTestUser(ctx context.Context, user TestUser, password string) error
	// UpdateTestUserFacts refreshes the directory id and role on an account the
	// platform already owns, and touches NOTHING else — the sealed password
	// above all. Rewriting the whole row instead would mean revealing the
	// password purely to seal it again, which both decrypts a credential for no
	// reason and fails the entire build for an account whose sealed password is
	// missing.
	UpdateTestUserFacts(ctx context.Context, username, thunderUserID, roleName string) error
	// SetTestUserPassword seals and stores a rotated password, stamping
	// rotated_at.
	SetTestUserPassword(ctx context.Context, username, password string) error
	// RevealTestUserPassword opens the sealed password. Every caller is a
	// deliberate disclosure — the validation credential vend, or the console's
	// explicit reveal action.
	RevealTestUserPassword(ctx context.Context, username string) (string, error)
	// DeleteTestUser forgets an account and every reference to it.
	DeleteTestUser(ctx context.Context, username string) error

	// -- project references ----------------------------------------------

	// ReplaceProjectRefs makes refs the complete set for this project, in one
	// transaction. The ensure calls it with the plan it just made real, so a
	// role dropped from a design stops being referenced — while the directory
	// object itself stands, per the additive-only rule.
	ReplaceProjectRefs(ctx context.Context, orgID, projectID string, refs []TestUserRef) error
	// ListProjectRefs returns this project's references, role-ordered.
	ListProjectRefs(ctx context.Context, orgID, projectID string) ([]TestUserRef, error)
	// ProjectsReferencing returns THIS ORG's projects that reference an account
	// — the console's "referencing projects" column. It is org-fenced even
	// though the account itself is shared: a project name is one org's data,
	// and the panel showing another org's project names would be a cross-tenant
	// disclosure the shared directory does not license.
	ProjectsReferencing(ctx context.Context, orgID, username string) ([]TestUserRef, error)
	// CountReferencing returns how many projects reference an account IN TOTAL,
	// across every org. A bare count, never names: it is what makes "others may
	// still be using this" sayable before a delete, and it is the minimum
	// disclosure that makes that warning true.
	CountReferencing(ctx context.Context, username string) (int, error)
}

// ErrNoPassword is returned when an account's sealed password is absent — a row
// written before the seal, or one whose password was never generated here.
var ErrNoPassword = errors.New("identity: no sealed password for this account")

type store struct {
	db     *gorm.DB
	cipher *secrets.ColumnCipher
}

// NewStore builds the persistence surface. cipher may be nil (the ColumnCipher
// passthrough contract), in which case passwords are stored as written — the
// same degradation every other sealed column in this codebase has.
func NewStore(db *gorm.DB, cipher *secrets.ColumnCipher) Store {
	return &store{db: db, cipher: cipher}
}

func (s *store) GetRole(ctx context.Context, name string) (*IdPRole, error) {
	var row IdPRole
	// Case-insensitive, matching how the rest of the platform compares role
	// names: two names differing only in case are one role.
	err := s.db.WithContext(ctx).Where("lower(name) = lower(?)", name).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get role %q: %w", name, err)
	}
	return &row, nil
}

func (s *store) ListRoles(ctx context.Context) ([]IdPRole, error) {
	var rows []IdPRole
	if err := s.db.WithContext(ctx).Order("name").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return rows, nil
}

func (s *store) UpsertRole(ctx context.Context, role IdPRole) error {
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		// created_by_* are NOT updated: they record who first declared the role,
		// and a second project adopting it does not take it over.
		DoUpdates: clause.AssignmentColumns([]string{"thunder_group_id", "updated_at"}),
	}).Create(&role).Error
	if err != nil {
		return fmt.Errorf("upsert role %q: %w", role.Name, err)
	}
	return nil
}

func (s *store) GetTestUser(ctx context.Context, username string) (*TestUser, error) {
	var row TestUser
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get test user %q: %w", username, err)
	}
	return &row, nil
}

func (s *store) UpsertTestUser(ctx context.Context, user TestUser, password string) error {
	sealed, err := s.seal(password)
	if err != nil {
		return err
	}
	user.PasswordSealed = sealed
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "username"}},
		DoUpdates: clause.AssignmentColumns([]string{"thunder_user_id", "role_name", "password_sealed", "email"}),
	}).Create(&user).Error
	if err != nil {
		return fmt.Errorf("upsert test user %q: %w", user.Username, err)
	}
	return nil
}

func (s *store) UpdateTestUserFacts(ctx context.Context, username, thunderUserID, roleName string) error {
	res := s.db.WithContext(ctx).Model(&TestUser{}).Where("username = ?", username).
		Updates(map[string]any{"thunder_user_id": thunderUserID, "role_name": roleName})
	if res.Error != nil {
		return fmt.Errorf("update facts for %q: %w", username, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("update facts for %q: no such account", username)
	}
	return nil
}

func (s *store) SetTestUserPassword(ctx context.Context, username, password string) error {
	sealed, err := s.seal(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&TestUser{}).Where("username = ?", username).
		Updates(map[string]any{"password_sealed": sealed, "rotated_at": now})
	if res.Error != nil {
		return fmt.Errorf("set password for %q: %w", username, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("set password for %q: no such account", username)
	}
	return nil
}

func (s *store) RevealTestUserPassword(ctx context.Context, username string) (string, error) {
	row, err := s.GetTestUser(ctx, username)
	if err != nil {
		return "", err
	}
	if row == nil || strings.TrimSpace(row.PasswordSealed) == "" {
		return "", ErrNoPassword
	}
	// Open, not OpenTolerant. There is no migration window for this column —
	// every row was written sealed by this package — so a decrypt failure means
	// the credential-encryption-key changed under us. OpenTolerant would answer
	// that by handing the caller the base64 ciphertext AS the password, which
	// the validation runner would then dutifully type into a login form.
	plain, err := s.cipher.Open(row.PasswordSealed)
	if err != nil {
		return "", fmt.Errorf("open password for %q: %w", username, err)
	}
	return string(plain), nil
}

func (s *store) DeleteTestUser(ctx context.Context, username string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("username = ?", username).Delete(&TestUserRef{}).Error; err != nil {
			return fmt.Errorf("delete refs for %q: %w", username, err)
		}
		if err := tx.Where("username = ?", username).Delete(&TestUser{}).Error; err != nil {
			return fmt.Errorf("delete test user %q: %w", username, err)
		}
		return nil
	})
}

func (s *store) ReplaceProjectRefs(ctx context.Context, orgID, projectID string, refs []TestUserRef) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? AND project_id = ?", orgID, projectID).
			Delete(&TestUserRef{}).Error; err != nil {
			return fmt.Errorf("clear refs for %s/%s: %w", orgID, projectID, err)
		}
		if len(refs) == 0 {
			return nil
		}
		now := time.Now().UTC()
		for i := range refs {
			refs[i].OrgID = orgID
			refs[i].ProjectID = projectID
			refs[i].UpdatedAt = now
		}
		if err := tx.Create(&refs).Error; err != nil {
			return fmt.Errorf("write refs for %s/%s: %w", orgID, projectID, err)
		}
		return nil
	})
}

func (s *store) ListProjectRefs(ctx context.Context, orgID, projectID string) ([]TestUserRef, error) {
	var rows []TestUserRef
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Order("role_name, username").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list refs for %s/%s: %w", orgID, projectID, err)
	}
	return rows, nil
}

func (s *store) ProjectsReferencing(ctx context.Context, orgID, username string) ([]TestUserRef, error) {
	var rows []TestUserRef
	err := s.db.WithContext(ctx).Where("org_id = ? AND username = ?", orgID, username).
		Order("project_id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list projects referencing %q: %w", username, err)
	}
	return rows, nil
}

func (s *store) CountReferencing(ctx context.Context, username string) (int, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&TestUserRef{}).
		Where("username = ?", username).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count projects referencing %q: %w", username, err)
	}
	return int(n), nil
}

// seal encrypts a password for storage. An empty password seals to empty, which
// is how a row for an account the platform did not generate a password for is
// represented — RevealTestUserPassword answers ErrNoPassword for it.
func (s *store) seal(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	sealed, err := s.cipher.Seal([]byte(password))
	if err != nil {
		return "", fmt.Errorf("seal password: %w", err)
	}
	return sealed, nil
}
