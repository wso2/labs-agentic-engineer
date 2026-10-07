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

package reqspec

import (
	"regexp"
	"strings"
)

const (
	tagAssumed  = "assumed"
	tagBlocking = "blocking"
)

// One line of the contract (skills/prd-contract, "Lines"): an optional ID and
// "(was …)" record, the words, then sources, `Needs:` or `Applies to:`, and a
// closing tag. Sources and the clauses are found anywhere in the line, so a
// line written slightly out of order still reads.
type line struct {
	id, was   string
	text      string
	needs     []string
	appliesTo []string
	// tag is the closing tag's word, "" when there is none.
	tag string
}

var (
	leadRE      = regexp.MustCompile(`^(F\d+\.\d+|P\d+)(?:\s+|$)`)
	wasRE       = regexp.MustCompile(`^\(was (F\d+\.\d+|P\d+)\)\s*`)
	tagRE       = regexp.MustCompile(`(?:\*|_)(assumed|blocking)(?:\*|_)[\s.,;:]*$`)
	sourceRE    = regexp.MustCompile(`\[[^\[\]]+\]`)
	needsRE     = regexp.MustCompile(`(?i)\bneeds:\s*(F\d+(?:\s*,\s*F\d+)*)\.?`)
	needsLineRE = regexp.MustCompile(`(?i)^needs:`)
	appliesRE   = regexp.MustCompile(`(?i)\bapplies to:\s*(all|F\d+(?:\s*,\s*F\d+)*)\.?`)
	listIDRE    = regexp.MustCompile(`(?i)F\d+|all`)
	spaceRE     = regexp.MustCompile(`\s+`)
)

func parseLine(s string) line {
	var l line
	s = strings.TrimSpace(s)
	if m := leadRE.FindStringSubmatch(s); m != nil {
		l.id = m[1]
		s = s[len(m[0]):]
		if w := wasRE.FindStringSubmatch(s); w != nil {
			l.was = w[1]
			s = s[len(w[0]):]
		}
	}
	if m := tagRE.FindStringSubmatchIndex(s); m != nil {
		l.tag = s[m[2]:m[3]]
		s = s[:m[0]]
	}
	s = sourceRE.ReplaceAllString(s, " ")
	if m := needsRE.FindStringSubmatch(s); m != nil {
		l.needs = idList(m[1])
		s = strings.Replace(s, m[0], " ", 1)
	}
	if m := appliesRE.FindStringSubmatch(s); m != nil {
		l.appliesTo = idList(m[1])
		s = strings.Replace(s, m[0], " ", 1)
	}
	l.text = strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
	return l
}

func idList(s string) []string {
	ids := listIDRE.FindAllString(s, -1)
	for i, id := range ids {
		if strings.EqualFold(id, "all") {
			ids[i] = "all"
		}
	}
	return ids
}
