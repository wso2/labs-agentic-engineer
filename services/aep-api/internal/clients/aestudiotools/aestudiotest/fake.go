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

// Package aestudiotest is an in-memory stand-in for an org's ae-studio-tools,
// behind the same ports as aestudiotools.Adapter. Test support only.
package aestudiotest

import (
	"context"
	"errors"
	"io"
	"iter"
	"mime"
	"mime/multipart"
	"slices"
	"sync"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
)

// Operation names FailOp takes.
const (
	OpStartTurn      = "start-turn"
	OpPutReferences  = "put-references"
	OpGitHubIdentity = "github-identity"
)

// TurnCall is one StartTurn the fake saw.
type TurnCall struct {
	Ref     aestudiotools.RepoRef
	Request aestudiotools.TurnRequest
}

// Fake is the in-memory pod. The zero value is not usable; call New.
type Fake struct {
	mu         sync.Mutex
	fail       map[string]error
	turns      []TurnCall
	script     []aestudiotools.TurnEvent
	references map[aestudiotools.RepoRef][]string
	identities map[string]aestudiotools.GitHubIdentity
}

var (
	_ aestudiotools.Turns      = (*Fake)(nil)
	_ aestudiotools.References = (*Fake)(nil)
	_ aestudiotools.Identity   = (*Fake)(nil)
)

// New is an empty pod: turns complete at once, no references, no identity.
func New() *Fake {
	return &Fake{
		fail:       map[string]error{},
		references: map[aestudiotools.RepoRef][]string{},
		identities: map[string]aestudiotools.GitHubIdentity{},
	}
}

// FailOp makes every later call of op return err; a nil err clears it.
func (f *Fake) FailOp(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.fail, op)
		return
	}
	f.fail[op] = err
}

// ScriptTurn sets the events every later turn streams. Without a result
// event, the stream ends with {result, completed}.
func (f *Fake) ScriptTurn(events ...aestudiotools.TurnEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = slices.Clone(events)
}

// TurnCalls lists the turns started so far, in order.
func (f *Fake) TurnCalls() []TurnCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.turns)
}

// References lists the file names stored for ref by the last accepted
// upload, in upload order.
func (f *Fake) References(ref aestudiotools.RepoRef) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.references[ref])
}

// SetIdentity sets the org's GitHub identity.
func (f *Fake) SetIdentity(org string, id aestudiotools.GitHubIdentity) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identities[org] = id
}

func (f *Fake) failure(op string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fail[op]
}

// StartTurn records the call and streams the scripted events.
func (f *Fake) StartTurn(_ context.Context, ref aestudiotools.RepoRef, req aestudiotools.TurnRequest) (iter.Seq2[aestudiotools.TurnEvent, error], error) {
	if err := f.failure(OpStartTurn); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.turns = append(f.turns, TurnCall{Ref: ref, Request: req})
	events := slices.Clone(f.script)
	f.mu.Unlock()
	if len(events) == 0 || events[len(events)-1].Type != aestudiotools.EventResult {
		events = append(events, aestudiotools.TurnEvent{Type: aestudiotools.EventResult, Status: "completed"})
	}
	return func(yield func(aestudiotools.TurnEvent, error) bool) {
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}, nil
}

// PutReferences reads the multipart upload of field `files` and stores its
// file names for ref, replacing the previous set. It closes body, as the
// adapter does.
func (f *Fake) PutReferences(_ context.Context, ref aestudiotools.RepoRef, contentType string, body io.Reader) error {
	if c, ok := body.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}
	if err := f.failure(OpPutReferences); err != nil {
		return err
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return errors.New("aestudiotest: not a multipart content type")
	}
	mr := multipart.NewReader(body, params["boundary"])
	var names []string
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if p.FormName() != "files" {
			return errors.New("aestudiotest: unexpected field " + p.FormName())
		}
		if _, err := io.Copy(io.Discard, p); err != nil {
			return err
		}
		names = append(names, p.FileName())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.references[ref] = names
	return nil
}

// GitHubIdentity answers the identity SetIdentity gave the org.
func (f *Fake) GitHubIdentity(_ context.Context, org string) (aestudiotools.GitHubIdentity, error) {
	if err := f.failure(OpGitHubIdentity); err != nil {
		return aestudiotools.GitHubIdentity{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.identities[org]
	if !ok {
		return aestudiotools.GitHubIdentity{}, &aestudiotools.StatusError{Op: "get-github-identity", Status: 502, Code: "github_error"}
	}
	return id, nil
}
