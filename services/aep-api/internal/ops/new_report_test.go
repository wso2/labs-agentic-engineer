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
	"errors"
	"strings"
	"testing"
)

func validInput() NewReportInput {
	return NewReportInput{
		Project:        "proj",
		Title:          "Checkout 500s",
		Summary:        "Spike in 500s",
		Classification: "code-level",
		Diagnosis:      "npe in handler",
	}
}

func TestNewReport_Valid(t *testing.T) {
	r, err := NewReport("acme", validInput())
	if err != nil {
		t.Fatalf("NewReport: %v", err)
	}
	if r.OrgID != "acme" || r.Project != "proj" || r.Classification != "code-level" {
		t.Errorf("report = %+v, want org acme and the input's fields", r)
	}
}

func TestNewReport_MissingRequiredFields(t *testing.T) {
	cases := map[string]func(*NewReportInput){
		"project":        func(in *NewReportInput) { in.Project = "" },
		"title":          func(in *NewReportInput) { in.Title = "" },
		"summary":        func(in *NewReportInput) { in.Summary = "" },
		"diagnosis":      func(in *NewReportInput) { in.Diagnosis = "" },
		"classification": func(in *NewReportInput) { in.Classification = "" },
	}
	for field, blank := range cases {
		t.Run(field, func(t *testing.T) {
			in := validInput()
			blank(&in)
			_, err := NewReport("acme", in)
			if !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("err = %v, want ErrInvalidReport", err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("error %q does not name the missing field %q", err, field)
			}
		})
	}
}

func TestNewReport_InvalidClassification(t *testing.T) {
	in := validInput()
	in.Classification = "vibes"
	_, err := NewReport("acme", in)
	if !errors.Is(err, ErrInvalidReport) || !strings.Contains(err.Error(), "code-level") {
		t.Fatalf("err = %v, want ErrInvalidReport naming the allowed classifications", err)
	}
}
