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

package prototypespec

// references.go — the manifest rules a standalone JSON Schema cannot express,
// ported from the kit's references.ts. Same rules, same order, same codes,
// locations and messages (manifest-cases.json is the table both assert):
//
//  1. Ids: roles, states, screens and flows share one namespace. A repeat is
//     DUPLICATE_ID and ends validation (a reference into an ambiguous
//     namespace has no single target).
//  2. The entry screen is a screen.
//  3. Each screen's roles are declared.
//  4. Each flow's role is declared, and each step is a screen it reaches.

import (
	"fmt"
	"slices"
)

func referenceFindings(m *Manifest) []Finding {
	if duplicates := duplicateIDFindings(m); len(duplicates) > 0 {
		return duplicates
	}
	var findings []Finding
	report := func(location, format string, args ...any) {
		findings = append(findings, Finding{Code: CodeUnknownReference, Location: location, Message: fmt.Sprintf(format, args...)})
	}

	roles := map[string]bool{}
	for _, r := range m.Roles {
		roles[r.ID] = true
	}
	screens := map[string]*Screen{}
	for i := range m.Screens {
		screens[m.Screens[i].ID] = &m.Screens[i]
	}

	if screens[m.EntryScreen] == nil {
		report("entryScreen", "entryScreen %s is not one of the screens", quote(m.EntryScreen))
	}
	for i, s := range m.Screens {
		for j, r := range s.RoleIDs {
			if !roles[r] {
				report(fmt.Sprintf("screens[%d].roleIds[%d]", i, j), "role %s is not declared in roles", quote(r))
			}
		}
	}
	for i, f := range m.Flows {
		roleKnown := roles[f.RoleID]
		if !roleKnown {
			report(fmt.Sprintf("flows[%d].roleId", i), "role %s is not declared in roles", quote(f.RoleID))
		}
		for j, id := range f.ScreenIDs {
			location := fmt.Sprintf("flows[%d].screenIds[%d]", i, j)
			screen := screens[id]
			switch {
			case screen == nil:
				report(location, "screen %s is not one of the screens", quote(id))
			case roleKnown && !slices.Contains(screen.RoleIDs, f.RoleID):
				report(location, "screen %s is not one role %s reaches; add the role to the screen's roleIds or take the step out of the flow", quote(id), quote(f.RoleID))
			}
		}
	}
	return findings
}

func duplicateIDFindings(m *Manifest) []Finding {
	first := map[string]string{}
	var findings []Finding
	visit := func(list string, i int, id string) {
		location := fmt.Sprintf("%s[%d].id", list, i)
		if seen, ok := first[id]; ok {
			findings = append(findings, Finding{
				Code:     CodeDuplicateID,
				Location: location,
				Message:  fmt.Sprintf("id %s is already used at %s; role, state, screen and flow ids are unique together", quote(id), seen),
			})
			return
		}
		first[id] = location
	}
	for i, r := range m.Roles {
		visit("roles", i, r.ID)
	}
	for i, s := range m.States {
		visit("states", i, s.ID)
	}
	for i, s := range m.Screens {
		visit("screens", i, s.ID)
	}
	for i, f := range m.Flows {
		visit("flows", i, f.ID)
	}
	return findings
}
