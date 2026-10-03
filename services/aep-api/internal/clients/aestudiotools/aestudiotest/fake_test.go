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

package aestudiotest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
)

var ref = aestudiotools.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}

func TestFake_Turns(t *testing.T) {
	f := New()
	f.ScriptTurn(
		aestudiotools.TurnEvent{Type: aestudiotools.EventTaskOp, Op: "plan"},
		aestudiotools.TurnEvent{Type: aestudiotools.EventKeepAlive},
	)
	seq, err := f.StartTurn(context.Background(), ref, aestudiotools.TurnRequest{TurnID: "t-1", Kind: aestudiotools.TurnKindPlan})
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for ev, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, ev.Type)
	}
	if !slices.Equal(types, []string{"task-op", "keep-alive", "result"}) {
		t.Fatalf("events = %v, want the script then a completed result", types)
	}
	if calls := f.TurnCalls(); len(calls) != 1 || calls[0].Ref != ref || calls[0].Request.TurnID != "t-1" {
		t.Fatalf("calls = %+v", calls)
	}
	f.FailOp(OpStartTurn, aestudiotools.ErrTurnInProgress)
	if _, err := f.StartTurn(context.Background(), ref, aestudiotools.TurnRequest{}); !errors.Is(err, aestudiotools.ErrTurnInProgress) {
		t.Fatalf("err = %v", err)
	}
	if len(f.TurnCalls()) != 1 {
		t.Fatal("a failed start is not recorded")
	}
}

func TestFake_References(t *testing.T) {
	f := New()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, n := range []string{"sketch.png", "brief.pdf"} {
		w, _ := mw.CreateFormFile("files", n)
		_, _ = io.WriteString(w, n)
	}
	_ = mw.Close()
	if err := f.PutReferences(context.Background(), ref, mw.FormDataContentType(), &buf); err != nil {
		t.Fatal(err)
	}
	if got := f.References(ref); !slices.Equal(got, []string{"sketch.png", "brief.pdf"}) {
		t.Fatalf("references = %v", got)
	}
	f.FailOp(OpPutReferences, aestudiotools.ErrReferenceRejected)
	if err := f.PutReferences(context.Background(), ref, mw.FormDataContentType(), &bytes.Buffer{}); !errors.Is(err, aestudiotools.ErrReferenceRejected) {
		t.Fatalf("err = %v", err)
	}
	if got := f.References(ref); len(got) != 2 {
		t.Fatalf("a refused upload changed the set: %v", got)
	}
}

func TestFake_Identity(t *testing.T) {
	f := New()
	if _, err := f.GitHubIdentity(context.Background(), "default"); err == nil {
		t.Fatal("want an error without an identity")
	}
	f.SetIdentity("default", aestudiotools.GitHubIdentity{Login: "acme-bot", ID: 1})
	if id, err := f.GitHubIdentity(context.Background(), "default"); err != nil || id.Login != "acme-bot" {
		t.Fatalf("id=%+v err=%v", id, err)
	}
}
