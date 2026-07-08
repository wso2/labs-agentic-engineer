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

package openchoreo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// releaseBindingWithCondition builds a minimal gen.ReleaseBinding carrying one
// condition — the shape IsComponentReady/releaseBindingReady reads.
func releaseBindingWithCondition(condType string, status gen.ConditionStatus) gen.ReleaseBinding {
	conds := []gen.Condition{{Type: condType, Status: status, Reason: "test"}}
	return gen.ReleaseBinding{Status: &gen.ReleaseBindingStatus{Conditions: &conds}}
}

func TestReleaseBindingReady(t *testing.T) {
	cases := []struct {
		name string
		rb   gen.ReleaseBinding
		want bool
	}{
		{"nil status", gen.ReleaseBinding{}, false},
		{"nil conditions", gen.ReleaseBinding{Status: &gen.ReleaseBindingStatus{}}, false},
		{"ready true", releaseBindingWithCondition("Ready", "True"), true},
		{"ready false", releaseBindingWithCondition("Ready", "False"), false},
		{"no ready condition", releaseBindingWithCondition("Progressing", "True"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := releaseBindingReady(tc.rb); got != tc.want {
				t.Errorf("releaseBindingReady(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsComponentReady(t *testing.T) {
	t.Run("ready binding present", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, gen.ReleaseBindingList{
				Items: []gen.ReleaseBinding{releaseBindingWithCondition("Ready", "True")},
			})
		}))
		defer srv.Close()

		c := NewComponentClient(Config{BaseURL: srv.URL})
		ready, err := c.IsComponentReady(context.Background(), "wc-abc", "proj1", "comp1")
		if err != nil {
			t.Fatalf("IsComponentReady: %v", err)
		}
		if !ready {
			t.Errorf("ready = false, want true")
		}
	})

	t.Run("no bindings yet", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, gen.ReleaseBindingList{})
		}))
		defer srv.Close()

		c := NewComponentClient(Config{BaseURL: srv.URL})
		ready, err := c.IsComponentReady(context.Background(), "wc-abc", "proj1", "comp1")
		if err != nil {
			t.Fatalf("IsComponentReady: %v", err)
		}
		if ready {
			t.Errorf("ready = true, want false (no bindings yet)")
		}
	})

	t.Run("not-ready binding", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, gen.ReleaseBindingList{
				Items: []gen.ReleaseBinding{releaseBindingWithCondition("Progressing", "True")},
			})
		}))
		defer srv.Close()

		c := NewComponentClient(Config{BaseURL: srv.URL})
		ready, err := c.IsComponentReady(context.Background(), "wc-abc", "proj1", "comp1")
		if err != nil {
			t.Fatalf("IsComponentReady: %v", err)
		}
		if ready {
			t.Errorf("ready = true, want false")
		}
	})

	t.Run("server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
		}))
		defer srv.Close()

		c := NewComponentClient(Config{BaseURL: srv.URL})
		if _, err := c.IsComponentReady(context.Background(), "wc-abc", "proj1", "comp1"); err == nil {
			t.Fatalf("expected an error on 500")
		}
	})
}
