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

package edge

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	ready := false
	h := NewHealth(func() bool { return ready })
	for _, c := range []struct {
		path string
		want int
	}{{"/healthz", 200}, {"/readyz", 503}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.want {
			t.Fatalf("%s = %d", c.path, rec.Code)
		}
	}
	ready = true
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != 200 {
		t.Fatalf("readyz after ready = %d", rec.Code)
	}
}

func TestHealth_OnlyGETAndKnownPaths(t *testing.T) {
	h := NewHealth(func() bool { return true })
	for _, c := range []struct {
		method, path string
		want         int
	}{{http.MethodPost, "/healthz", 405}, {http.MethodGet, "/", 404}, {http.MethodGet, "/metrics", 404}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Fatalf("%s %s = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
}
