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
	"reflect"
	"testing"
)

const cellFixture = `title Lunch Coordinator
version v1

# own components
component lunch-api as "Lunch API" service
component lunch-web web-application
component slack-notifier service
component orders-db database

east team-auth as "Thunder Auth" identity-server
south slack as "Slack" saas

lunch-web -> lunch-api
lunch-api -> orders-db
slack-notifier -> south slack | notifications
north -> lunch-web
east team-auth -> lunch-api
`

func TestParseCellFacts_FullFixture(t *testing.T) {
	facts, err := parseCellFacts(cellFixture)
	if err != nil {
		t.Fatalf("parseCellFacts: %v", err)
	}
	wantComponents := []CellComponent{
		{ID: "lunch-api", Type: "service"},
		{ID: "lunch-web", Type: "web-application"},
		{ID: "slack-notifier", Type: "service"},
		{ID: "orders-db", Type: "database"},
	}
	if !reflect.DeepEqual(facts.Components, wantComponents) {
		t.Errorf("components = %+v\nwant %+v", facts.Components, wantComponents)
	}
}

// Quoted labels may contain the word component / brackets — the tokenizer
// must not trip on them, and unknown statements are ignored (the TS parser is
// the authoritative validator; Go extracts facts permissively).
func TestParseCellFacts_PermissiveOnUnknownStatements(t *testing.T) {
	facts, err := parseCellFacts("something odd here\ncomponent api as \"An [api] component\" service")
	if err != nil {
		t.Fatalf("parseCellFacts: %v", err)
	}
	if len(facts.Components) != 1 || facts.Components[0].ID != "api" || facts.Components[0].Type != "service" {
		t.Errorf("components = %+v", facts.Components)
	}
}
