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

package spec

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ProjectConversation is the project's chat thread pointer (#430): which
// agents-service conversation is CURRENT for (org, project, use case). The
// row's ID is the conversation id the console addresses turns with — minted
// here, never by the client (the pre-#430 id was FE-chosen localStorage, which
// made every browser its own thread and "an interview is open" unknowable
// project-wide).
//
// Exactly one current row per scope, enforced by the partial unique index
// ux_project_conversations_current (migrate/project_conversations.go — the
// #420 admission pattern). Rotation demotes the current row and inserts a
// fresh one; demoted rows survive as the multi-conversation future's history
// and are never deleted here.
type ProjectConversation struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"conversationId"`
	OrgID     string `gorm:"not null;index" json:"-"`
	ProjectID string `gorm:"not null" json:"-"`
	UseCase   string `gorm:"not null" json:"-"`
	Current   bool   `gorm:"not null;default:true" json:"current"`
	// CreatedBy is the display identity of whoever first resolved (or rotated
	// into) the thread — informational, shown in the conversations listing.
	CreatedBy string    `gorm:"type:text" json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// TableName pins the table name (house convention: explicit, not inflected).
func (ProjectConversation) TableName() string { return "project_conversations" }

// ConversationRepository is the project_conversations store. Lookups miss with
// (nil, nil), matching the house convention.
type ConversationRepository interface {
	// ResolveCurrent returns the scope's current thread, creating it if the
	// scope has none — lazily, on first read, race-safe against the partial
	// unique (concurrent first resolvers converge on one row). createdBy
	// stamps only a row this call creates.
	ResolveCurrent(ctx context.Context, orgID, projectID, useCase, createdBy string) (*ProjectConversation, error)

	// Rotate demotes the scope's current thread (if any) and mints a fresh
	// one — the "New conversation" action, project-wide by design (#430 D4).
	Rotate(ctx context.Context, orgID, projectID, useCase, createdBy string) (*ProjectConversation, error)

	// IsCurrent reports whether id is the scope's current thread — the turn
	// admission fence behind the single-era 409 (see StartTurn).
	IsCurrent(ctx context.Context, orgID, projectID, useCase, id string) (bool, error)

	// Exists reports whether id names ANY of the scope's threads, current or
	// demoted — the rehydrate read: a known-but-turn-less thread answers an
	// empty history, never a 404 (which the console must treat as failure).
	Exists(ctx context.Context, orgID, projectID, useCase, id string) (bool, error)
}

type conversationRepository struct{ db *gorm.DB }

// NewConversationRepository builds the project_conversations store.
func NewConversationRepository(db *gorm.DB) ConversationRepository {
	return &conversationRepository{db: db}
}

func (r *conversationRepository) getCurrent(db *gorm.DB, orgID, projectID, useCase string) (*ProjectConversation, error) {
	var row ProjectConversation
	err := db.
		Where("org_id = ? AND project_id = ? AND use_case = ? AND current", orgID, projectID, useCase).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// lockScope serializes the scope's WRITERS with a Postgres advisory
// transaction lock (released at commit/rollback, multi-replica safe). Under
// READ COMMITTED a concurrent writer's freshly committed current row is a
// phantom to this transaction's UPDATE/SELECT — the demote matches nothing,
// the re-check sees nothing, and a bare insert then collides with the partial
// unique and would surface as a 500. Retry loops lose under real contention
// (a racer can lose every round to a fresh winner — observed at 6 concurrent
// rotates); the lock removes the race instead of pacing it. The partial
// unique index stays as the cross-path backstop.
func lockScope(tx *gorm.DB, orgID, projectID, useCase string) error {
	return tx.Exec(
		`SELECT pg_advisory_xact_lock(hashtextextended('project_conversations:' || ? || '/' || ? || '/' || ?, 0))`,
		orgID, projectID, useCase,
	).Error
}

func (r *conversationRepository) ResolveCurrent(ctx context.Context, orgID, projectID, useCase, createdBy string) (*ProjectConversation, error) {
	// Fast path: the common case is a read of an existing current row — no
	// lock, no transaction.
	if row, err := r.getCurrent(r.db.WithContext(ctx), orgID, projectID, useCase); err != nil || row != nil {
		return row, err
	}
	row := &ProjectConversation{
		OrgID:     orgID,
		ProjectID: projectID,
		UseCase:   useCase,
		Current:   true,
		CreatedBy: createdBy,
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScope(tx, orgID, projectID, useCase); err != nil {
			return err
		}
		// Re-check under the lock: the racer that held it may have created.
		existing, err := r.getCurrent(tx, orgID, projectID, useCase)
		if err != nil {
			return err
		}
		if existing != nil {
			row = existing
			return nil
		}
		return tx.Create(row).Error
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (r *conversationRepository) Rotate(ctx context.Context, orgID, projectID, useCase, createdBy string) (*ProjectConversation, error) {
	fresh := &ProjectConversation{
		OrgID:     orgID,
		ProjectID: projectID,
		UseCase:   useCase,
		Current:   true,
		CreatedBy: createdBy,
	}
	// Concurrent rotates serialize on the scope lock: each demotes the
	// previous winner's row and mints its own — as many rotations as clicks,
	// last one wins, every thread survives. The intended semantics of two
	// people clicking New conversation.
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockScope(tx, orgID, projectID, useCase); err != nil {
			return err
		}
		if err := tx.Model(&ProjectConversation{}).
			Where("org_id = ? AND project_id = ? AND use_case = ? AND current", orgID, projectID, useCase).
			Update("current", false).Error; err != nil {
			return err
		}
		return tx.Create(fresh).Error
	})
	if err != nil {
		return nil, err
	}
	return fresh, nil
}

// The id columns are uuid-typed, but the id ARRIVES as a client string that
// validConversationID bounds far looser than uuid syntax — a Postgres cast
// error on `uuid = 'abc123'` would surface as a 500 where the contract owes a
// 409/404. Comparing as text makes a non-uuid id simply never match.

func (r *conversationRepository) IsCurrent(ctx context.Context, orgID, projectID, useCase, id string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&ProjectConversation{}).
		Where("org_id = ? AND project_id = ? AND use_case = ? AND current AND id::text = ?", orgID, projectID, useCase, id).
		Count(&n).Error
	return n > 0, err
}

func (r *conversationRepository) Exists(ctx context.Context, orgID, projectID, useCase, id string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&ProjectConversation{}).
		Where("org_id = ? AND project_id = ? AND use_case = ? AND id::text = ?", orgID, projectID, useCase, id).
		Count(&n).Error
	return n > 0, err
}
