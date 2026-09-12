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

package app

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

func TestSecurityJSONReader_SpecTagUsesGetDesignAtSpecTag(t *testing.T) {
	var specTag, head bool
	art := &artifactstest.FakeArtifactService{
		GetDesignAtTagFunc: func(_ context.Context, _, _, tag string) (map[string]string, error) {
			specTag = true
			if tag != "v3" {
				t.Fatalf("spec tag = %q, want v3", tag)
			}
			return map[string]string{securityspec.BundleKey: `{"ok":true}`}, nil
		},
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			head = true
			return map[string]string{securityspec.BundleKey: `{"ok":true}`}, nil
		},
		// GetDesignAtTagFunc left unset: calling it panics. A build's v<N> tag
		// must never go through the design-revision parser.
	}
	r := securityJSONReader{art: art}

	raw, err := r.ReadSecurityJSON(context.Background(), "org", "proj", "v3")
	if err != nil {
		t.Fatalf("ReadSecurityJSON(v3): %v", err)
	}
	if !specTag || head {
		t.Fatalf("spec tag must use GetDesignAtSpecTag only (specTag=%v head=%v)", specTag, head)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("raw = %q", raw)
	}

	specTag, head = false, false
	raw, err = r.ReadSecurityJSON(context.Background(), "org", "proj", "")
	if err != nil {
		t.Fatalf("ReadSecurityJSON(HEAD): %v", err)
	}
	if !head || specTag {
		t.Fatalf("empty tag must use ListDesignFiles only (head=%v specTag=%v)", head, specTag)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("HEAD raw = %q", raw)
	}
}

func TestSecurityJSONReader_AbsentFileIsNil(t *testing.T) {
	art := &artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return map[string]string{"design.json": "{}"}, nil
		},
	}
	raw, err := securityJSONReader{art: art}.ReadSecurityJSON(context.Background(), "org", "proj", "")
	if err != nil {
		t.Fatalf("absent security.json must not error, got %v", err)
	}
	if raw != nil {
		t.Fatalf("absent file must return nil bytes, got %q", raw)
	}
}
