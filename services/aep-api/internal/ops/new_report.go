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

package ops

import (
	"fmt"
	"time"
)

// validClassifications is the closed set the handoff agent may send.
var validClassifications = map[string]bool{
	"code-level":   true,
	"config-level": true,
	"mixed":        true,
	"none":         true,
}

// NewReportInput is a report as a writer submits it; NewReport validates it.
type NewReportInput struct {
	Project, Component, Title, Summary, Classification, Diagnosis string
	IssueNumber                                                   *int64
	IssueURL, IssueTitle, IssueExcerpt                            string
	Dispatched, Deployed                                          bool
	DeployedAt                                                    *time.Time
}

// NewReport validates in and builds the report for org (ErrInvalidReport on a
// missing field or an unknown classification). Fields the contract marks
// required are enforced here rather than left to a DB NOT NULL error, so the
// caller gets a precise 400. org comes from the verified credential, never
// from the input.
func NewReport(org string, in NewReportInput) (*RcaAgentReport, error) {
	var missing []string
	if in.Project == "" {
		missing = append(missing, "project")
	}
	if in.Title == "" {
		missing = append(missing, "title")
	}
	if in.Summary == "" {
		missing = append(missing, "summary")
	}
	if in.Diagnosis == "" {
		missing = append(missing, "diagnosis")
	}
	if in.Classification == "" {
		missing = append(missing, "classification")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: missing required field(s): %v", ErrInvalidReport, missing)
	}
	if !validClassifications[in.Classification] {
		return nil, fmt.Errorf("%w: classification %q must be one of code-level, config-level, mixed, none",
			ErrInvalidReport, in.Classification)
	}
	return &RcaAgentReport{
		OrgID:          org,
		Project:        in.Project,
		Component:      in.Component,
		Title:          in.Title,
		Summary:        in.Summary,
		Classification: in.Classification,
		Diagnosis:      in.Diagnosis,
		IssueNumber:    in.IssueNumber,
		IssueURL:       in.IssueURL,
		IssueTitle:     in.IssueTitle,
		IssueExcerpt:   in.IssueExcerpt,
		Dispatched:     in.Dispatched,
		Deployed:       in.Deployed,
		DeployedAt:     in.DeployedAt,
	}, nil
}
