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

package kubeauth

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHeader_StaticBearerWins(t *testing.T) {
	h, err := NewAuthorizer("static", writeFile(t, "token", "from-file")).Header()
	if err != nil || h != "Bearer static" {
		t.Fatalf("Header = %q, %v; want the static bearer", h, err)
	}
}

func TestHeader_NoneConfigured(t *testing.T) {
	h, err := NewAuthorizer("", "").Header()
	if err != nil || h != "" {
		t.Fatalf("Header = %q, %v; want no header", h, err)
	}
}

func TestHeader_TokenFileReReadAndTrimmed(t *testing.T) {
	p := writeFile(t, "token", " first \n")
	a := NewAuthorizer("", p)
	if h, err := a.Header(); err != nil || h != "Bearer first" {
		t.Fatalf("Header = %q, %v", h, err)
	}
	if err := os.WriteFile(p, []byte("rotated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, err := a.Header(); err != nil || h != "Bearer rotated" {
		t.Fatalf("Header = %q, %v; want the rotated token", h, err)
	}
}

func TestHeader_TokenFileErrors(t *testing.T) {
	if _, err := NewAuthorizer("", filepath.Join(t.TempDir(), "missing")).Header(); err == nil ||
		!strings.Contains(err.Error(), "read token file") {
		t.Errorf("missing file: err = %v", err)
	}
	if _, err := NewAuthorizer("", writeFile(t, "token", " \n")).Header(); err == nil ||
		!strings.Contains(err.Error(), "token file is empty") {
		t.Errorf("empty file: err = %v", err)
	}
}

func TestTransport_NoCAFileIsDefault(t *testing.T) {
	tr, err := Transport("")
	if err != nil || tr != nil {
		t.Fatalf("Transport(\"\") = %v, %v; want nil (system roots)", tr, err)
	}
}

func TestTransport_CAFileErrors(t *testing.T) {
	cases := map[string]struct{ path, want string }{
		"unreadable": {filepath.Join(t.TempDir(), "missing.crt"), "read CA file"},
		"empty":      {writeFile(t, "empty.crt", ""), "is empty"},
		"not PEM":    {writeFile(t, "bad.crt", "not-a-cert"), "not valid PEM"},
	}
	for name, tc := range cases {
		if _, err := Transport(tc.path); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestTransport_TrustsTheCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.TLS.Certificates[0].Certificate[0]})
	tr, err := Transport(writeFile(t, "ca.crt", string(ca)))
	if err != nil {
		t.Fatalf("Transport: %v", err)
	}
	resp, err := (&http.Client{Transport: tr}).Get(srv.URL)
	if err != nil {
		t.Fatalf("GET over the CA's transport: %v", err)
	}
	_ = resp.Body.Close()
}
