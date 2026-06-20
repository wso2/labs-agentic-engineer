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

// Package k8sname converts human-readable names into RFC 1123 compliant
// Kubernetes resource names. It is a leaf platform helper with no
// dependencies outside the standard library.
package k8sname

import (
	"regexp"
	"strings"
)

// invalidNameChars strips anything that isn't lowercase alphanumeric or a hyphen.
var invalidNameChars = regexp.MustCompile(`[^a-z0-9-]`)

// ToK8sName converts a human-readable name to an RFC 1123 compliant k8s name.
func ToK8sName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = invalidNameChars.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "component"
	}
	return s
}
