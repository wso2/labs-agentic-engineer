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

// Package config reads ae-studio-tools' settings from its pod env.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Config is what ae-studio-tools reads from its pod env (ticket 08 §3). Each
// later phase adds the keys its feature reads; nothing is read before it is used.
type Config struct {
	OrgID, OrgHandle       string
	IDPIssuer, IDPJWKSURL  string
	UserAudiences          []string
	M2MClientID            string
	GitHubPAT              string
	WebhookSecret          string
	ListenPort, HealthPort int
}

// ErrSecretRevMismatch means the mounted Secret is not the revision the pod
// spec was rendered for.
var ErrSecretRevMismatch = errors.New("secret revision mismatch")

const (
	defaultListenPort = 8082
	defaultHealthPort = 9082
)

// Load reads every key and reports all missing or invalid ones in one error,
// so a misconfigured pod names everything wrong with it in a single restart.
// The error names keys only, never values.
func Load(getenv func(string) string) (Config, error) {
	var problems []string
	req := func(k string) string {
		v := strings.TrimSpace(getenv(k))
		if v == "" {
			problems = append(problems, "missing "+k)
		}
		return v
	}
	port := func(k string, d int) int {
		v := strings.TrimSpace(getenv(k))
		if v == "" {
			return d
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			problems = append(problems, "invalid "+k)
			return 0
		}
		return n
	}
	c := Config{
		OrgID:         req("AE_ORG_ID"),
		OrgHandle:     req("AE_ORG_HANDLE"),
		IDPIssuer:     req("AE_IDP_ISSUER"),
		IDPJWKSURL:    req("AE_IDP_JWKS_URL"),
		M2MClientID:   req("AE_M2M_CLIENT_ID"),
		GitHubPAT:     req("GITHUB_PAT"),
		WebhookSecret: req("GITHUB_WEBHOOK_SECRET"),
		ListenPort:    port("AE_LISTEN_PORT", defaultListenPort),
		HealthPort:    port("AE_HEALTH_PORT", defaultHealthPort),
	}
	c.UserAudiences = splitList(getenv("AE_USER_AUDIENCES"))
	if len(c.UserAudiences) == 0 {
		problems = append(problems, "missing AE_USER_AUDIENCES")
	}
	if len(problems) > 0 {
		return Config{}, fmt.Errorf("ae-studio-tools env: %s", strings.Join(problems, ", "))
	}
	return c, nil
}

// CheckSecretRev enforces 08 §7: the container refuses to start on a Secret
// ESO has not refreshed yet. kubelet restarts it until the revs match. The
// revisions are hashes of reference names, not secrets, so the error may
// print them.
func CheckSecretRev(getenv func(string) string) error {
	got, want := getenv("AE_SECRET_REV"), getenv("AE_EXPECTED_SECRET_REV")
	if got != want {
		return fmt.Errorf("%w: secret has %q, pod expects %q", ErrSecretRevMismatch, got, want)
	}
	return nil
}

// splitList splits a comma list, trimming entries and dropping empty ones.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
