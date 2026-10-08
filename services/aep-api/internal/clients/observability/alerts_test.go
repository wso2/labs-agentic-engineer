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

package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type staticToken string

func (s staticToken) Token() (string, error) { return string(s), nil }

func TestAlertQuerierRecentAlert(t *testing.T) {
	var got alertsQueryRequest
	var gotAuth string
	status, body := http.StatusOK, `{"alerts":[{"alertId":"a1"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1alpha1/alerts/query" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	q := NewAlertQuerier(srv.URL+"/", staticToken("svc"))
	ctx, since := context.Background(), time.Now().Add(-time.Hour)

	ok, err := q.RecentAlert(ctx, "acme", "shop", "shop-api", since)
	if err != nil || !ok {
		t.Fatalf("an alert on record: ok=%v err=%v", ok, err)
	}
	if gotAuth != "Bearer svc" || got.SearchScope != (alertSearchScope{Namespace: "acme", Project: "shop", Component: "shop-api"}) {
		t.Fatalf("sent auth %q scope %+v", gotAuth, got.SearchScope)
	}

	for name, tc := range map[string]struct {
		status  int
		body    string
		want    bool
		wantErr bool
	}{
		"no alert":        {http.StatusOK, `{"alerts":[]}`, false, false},
		"unknown scope":   {http.StatusBadRequest, `{"errorCode":"SCOPE_NOT_FOUND"}`, false, false},
		"bad request":     {http.StatusBadRequest, `{"errorCode":"VALIDATION_ERROR"}`, false, true},
		"forbidden":       {http.StatusForbidden, `{}`, false, true},
		"observer broken": {http.StatusInternalServerError, `{}`, false, true},
	} {
		status, body = tc.status, tc.body
		ok, err := q.RecentAlert(ctx, "acme", "shop", "", since)
		if ok != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("%s: ok=%v err=%v, want ok=%v err=%v", name, ok, err, tc.want, tc.wantErr)
		}
	}
}
