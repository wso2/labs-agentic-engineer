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

// issue_search.go — recall-biased ranking for list-issues' q= keyword search.
// Serves the SRE/RCA handoff's search_related_issues tool (issues/sre_mcp.go):
// surface lexically-related issues before filing a new one; precision is the
// LLM caller's job.

package sourcecontrol

import (
	"sort"
	"strings"
	"unicode"
)

// maxRankedIssues caps how many candidates we hand back for a keyword search,
// so a large repo can't flood the caller (the SRE/RCA handoff agent, which
// then judges semantic relatedness itself). Highest-scoring first.
const maxRankedIssues = 25

// issueSearchStopwords are dropped from the query so generic words don't
// match everything. Deliberately small — domain terms (timeout, service1,
// oomkilled, …) must survive.
var issueSearchStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "add": true,
	"fix": true, "when": true, "from": true, "this": true, "that": true,
	"into": true, "via": true, "issue": true, "error": true, "errors": true,
}

// RankIssuesByQuery replaces the old single-substring filter, which returned
// nothing whenever the caller passed a multi-word query (the exact miss that
// left same-incident issues unlinked): "service1 timeout retry" was never a
// literal substring of any issue. Now the query is tokenised and each issue
// is scored by how many DISTINCT query terms it contains (title matches count
// double), so a natural-language query still surfaces lexically-overlapping
// candidates. Precision is intentionally left to the LLM handoff agent, which
// reads the returned title/body and judges true semantic relatedness — this
// layer only has to get real candidates in front of it (recall), not decide.
//
// Empty/all-stopword query → return everything (unchanged contract for a
// "list all" call). No overlap → empty, same as before.
func RankIssuesByQuery(issues []IssueInfo, query string) []IssueInfo {
	terms := tokenizeIssueQuery(query)
	if len(terms) == 0 {
		return issues
	}
	type scored struct {
		iss   IssueInfo
		score int
	}
	ranked := make([]scored, 0, len(issues))
	for _, iss := range issues {
		title := strings.ToLower(iss.Title)
		body := strings.ToLower(iss.Body)
		score := 0
		for t := range terms {
			if strings.Contains(title, t) {
				score += 2
			} else if strings.Contains(body, t) {
				score++
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{iss, score})
		}
	}
	// Stable sort by score desc so equal-scoring issues keep GitHub's order
	// (newest first) — makes the output deterministic for tests and callers.
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > maxRankedIssues {
		ranked = ranked[:maxRankedIssues]
	}
	out := make([]IssueInfo, len(ranked))
	for i, r := range ranked {
		out[i] = r.iss
	}
	return out
}

// tokenizeIssueQuery lowercases the query, splits on non-alphanumeric
// boundaries, and drops stopwords and tokens shorter than 3 chars. Returns a
// set so a repeated word doesn't inflate an issue's score.
func tokenizeIssueQuery(query string) map[string]bool {
	terms := make(map[string]bool)
	for _, tok := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(tok) < 3 || issueSearchStopwords[tok] {
			continue
		}
		terms[tok] = true
	}
	return terms
}
