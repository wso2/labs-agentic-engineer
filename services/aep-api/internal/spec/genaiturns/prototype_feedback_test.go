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

package genaiturns

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

func strPtr(s string) *string { return &s }

func feedbackAnnotation(id string) gen.PrototypeAnnotationInput {
	return gen.PrototypeAnnotationInput{
		ID:                     id,
		PrototypeSchemaVersion: 1,
		ScreenID:               "screen.queue",
		FlowID:                 strPtr("flow.approve"),
		StateID:                "state.default",
		ComponentIds:           []string{"queue.table", "queue.approve"},
		Request:                "Show the submitter's department",
	}
}

func feedbackFixture() *gen.PrototypeFeedbackInput {
	whole := feedbackAnnotation("ann-2")
	whole.FlowID = nil
	whole.ComponentIds = []string{}
	return &gen.PrototypeFeedbackInput{
		PrototypePath: "specs/design/components/portal/prototype.json",
		Annotations:   []gen.PrototypeAnnotationInput{feedbackAnnotation("ann-1"), whole},
	}
}

// Every field reaches the agents service exactly as the console sent it: the
// BFF validates and forwards, it never words anything.
func TestPrototypeFeedbackForwardsEveryFieldUnchanged(t *testing.T) {
	block, err := prototypeFeedbackFromJSON(feedbackFixture(), "/prototype", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &agentsvc.PrototypeFeedbackBlock{
		PrototypePath: "specs/design/components/portal/prototype.json",
		Annotations: []agentsvc.PrototypeAnnotationBlock{
			{ID: "ann-1", PrototypeSchemaVersion: 1, ScreenID: "screen.queue", FlowID: strPtr("flow.approve"),
				StateID: "state.default", ComponentIDs: []string{"queue.table", "queue.approve"}, Request: "Show the submitter's department"},
			{ID: "ann-2", PrototypeSchemaVersion: 1, ScreenID: "screen.queue", FlowID: nil,
				StateID: "state.default", ComponentIDs: []string{}, Request: "Show the submitter's department"},
		},
	}
	if !reflect.DeepEqual(block, want) {
		t.Fatalf("forwarded block = %+v, want %+v", block, want)
	}
}

// No batch, no block — an ordinary turn reaches the agents service
// byte-identical to one sent before this channel existed.
func TestPrototypeFeedbackAbsent(t *testing.T) {
	block, err := prototypeFeedbackFromJSON(nil, "hello", true)
	if err != nil || block != nil {
		t.Fatalf("absent batch = (%v, %v), want (nil, nil)", block, err)
	}
}

// A malformed batch is a 400 with a sentence naming what is wrong.
func TestPrototypeFeedbackRejectsMalformedBatches(t *testing.T) {
	cases := []struct {
		name        string
		mutate      func(f *gen.PrototypeFeedbackInput)
		instruction string
		aimed       bool
		want        string
	}{
		{"not a /prototype instruction", nil, "/design", false, "only valid on a /prototype instruction"},
		{"text after the command", nil, "/prototype make it blue", false, "only valid on a /prototype instruction"},
		{"with document aiming", nil, "/prototype", true, "cannot be combined with anchor or intent"},
		{"empty batch", func(f *gen.PrototypeFeedbackInput) { f.Annotations = nil }, "/prototype", false, "1 to 50 annotations"},
		{"over 50 annotations", func(f *gen.PrototypeFeedbackInput) {
			f.Annotations = nil
			for i := 0; i <= maxPrototypeAnnotations; i++ {
				f.Annotations = append(f.Annotations, feedbackAnnotation(fmt.Sprintf("a%d", i)))
			}
		}, "/prototype", false, "1 to 50 annotations"},
		{"duplicate annotation ids", func(f *gen.PrototypeFeedbackInput) { f.Annotations[1].ID = "ann-1" }, "/prototype", false, "duplicate annotation id"},
		{"over 50 component ids", func(f *gen.PrototypeFeedbackInput) {
			f.Annotations[0].ComponentIds = nil
			for i := 0; i <= maxPrototypeComponentIDs; i++ {
				f.Annotations[0].ComponentIds = append(f.Annotations[0].ComponentIds, fmt.Sprintf("c%d", i))
			}
		}, "/prototype", false, "at most 50 componentIds"},
		{"path outside the component slot", func(f *gen.PrototypeFeedbackInput) { f.PrototypePath = "specs/design/prototype.json" }, "/prototype", false, "prototypePath"},
		{"path escaping the slot", func(f *gen.PrototypeFeedbackInput) { f.PrototypePath = "specs/design/components/../prototype.json" }, "/prototype", false, "prototypePath"},
		{"nested path", func(f *gen.PrototypeFeedbackInput) { f.PrototypePath = "specs/design/components/a/b/prototype.json" }, "/prototype", false, "prototypePath"},
		{"unsupported schema version", func(f *gen.PrototypeFeedbackInput) { f.Annotations[0].PrototypeSchemaVersion = 2 }, "/prototype", false, "prototypeSchemaVersion"},
		{"blank request", func(f *gen.PrototypeFeedbackInput) { f.Annotations[0].Request = "  " }, "/prototype", false, "needs an id, screenId, stateId and request"},
		{"blank screen", func(f *gen.PrototypeFeedbackInput) { f.Annotations[0].ScreenID = "" }, "/prototype", false, "needs an id, screenId, stateId and request"},
		{"blank flow", func(f *gen.PrototypeFeedbackInput) { f.Annotations[0].FlowID = strPtr("") }, "/prototype", false, "flowId"},
		{"blank component id", func(f *gen.PrototypeFeedbackInput) { f.Annotations[0].ComponentIds = []string{""} }, "/prototype", false, "componentIds"},
		{"oversized request", func(f *gen.PrototypeFeedbackInput) {
			f.Annotations[0].Request = strings.Repeat("x", maxPrototypeRequestLen+1)
		}, "/prototype", false, "size limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := feedbackFixture()
			if tc.mutate != nil {
				tc.mutate(f)
			}
			_, err := prototypeFeedbackFromJSON(f, tc.instruction, tc.aimed)
			if err == nil {
				t.Fatal("expected a rejection")
			}
			var ae *apierr.Error
			if !errors.As(err, &ae) || ae.Status != 400 {
				t.Fatalf("want a 400 apierr, got %v", err)
			}
			if !strings.Contains(ae.Message, tc.want) {
				t.Fatalf("message %q does not mention %q", ae.Message, tc.want)
			}
		})
	}
}
