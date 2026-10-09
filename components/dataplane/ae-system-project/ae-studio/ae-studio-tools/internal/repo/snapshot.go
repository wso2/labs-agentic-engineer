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

package repo

import (
	"context"
	"strings"
)

// mirrorSnapshot is the Snapshot implementation backed by the bare mirror at
// one commit. It is handed to Mutate's fn as Tx.Base() while the exclusive
// flock is already held, so its reads deliberately take no lock of their own
// (flock is not re-entrant across separate fds). It captures the Mutate ctx;
// it is only valid for the duration of that fn invocation.
type mirrorSnapshot struct {
	ctx    context.Context
	e      *Engine
	p      repoPaths
	commit string
}

var _ Snapshot = (*mirrorSnapshot)(nil)

func (s *mirrorSnapshot) CommitSHA() string { return s.commit }

// Read returns content + blob sha of rel in the base tree; ErrPathNotFound
// when absent — the input to per-file baseSha precondition checks.
func (s *mirrorSnapshot) Read(rel string) ([]byte, string, error) {
	return s.e.readBlobAt(s.ctx, s.p, s.commit, rel)
}

// Walk visits every blob under prefix ("" = whole tree) with its blob sha.
func (s *mirrorSnapshot) Walk(prefix string, fn func(rel, blobSHA string) error) error {
	entries, err := s.e.lsTree(s.ctx, s.p, s.commit)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if prefix != "" && !strings.HasPrefix(entry.Path, prefix) {
			continue
		}
		if err := fn(entry.Path, entry.SHA); err != nil {
			return err
		}
	}
	return nil
}

// txOp is one staged Write or Delete.
type txOp struct {
	path    string
	content []byte
	delete  bool
}

// tx is the Tx implementation: an ordered op log applied to a throwaway
// index by Mutate after fn returns. Order preserved — the last op on a path
// wins, exactly like sequential update-index calls.
type tx struct {
	base *mirrorSnapshot
	ops  []txOp
}

var _ Tx = (*tx)(nil)

func (t *tx) Base() Snapshot { return t.base }

func (t *tx) Write(rel string, content []byte) {
	// Copy defensively — the commit happens after fn returns, and the caller
	// may reuse its buffer.
	c := make([]byte, len(content))
	copy(c, content)
	t.ops = append(t.ops, txOp{path: rel, content: c})
}

func (t *tx) Delete(rel string) {
	t.ops = append(t.ops, txOp{path: rel, delete: true})
}
