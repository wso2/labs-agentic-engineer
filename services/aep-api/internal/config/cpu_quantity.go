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

package config

import (
	"errors"
	"strconv"
	"strings"
)

// maxCPUDecimals is the finest CPU granularity Kubernetes accepts (1m).
const maxCPUDecimals = 3

// CodingAgentCPUCeilingMillicores is the coding-agent ComponentType's cpuLimit
// default ("3"); a request above it makes a pod Kubernetes refuses.
const CodingAgentCPUCeilingMillicores = 3000

var errBadCPUQuantity = errors.New("not a positive CPU quantity (<int>m millicores or decimal cores, e.g. 250m, 0.5, 2)")

// ParseCPUMillicores parses a Kubernetes CPU quantity in the two forms an
// operator writes, "<int>m" millicores or decimal cores ("0.25", "1", "1.5"),
// into millicores. It rejects zero, negatives, fractions of a millicore and any
// other suffix or notation. The error never carries the input, so a caller can
// wrap it with only the env key. aep-api carries no k8s.io dependency, so this
// is the one place CPU envs are validated.
func ParseCPUMillicores(s string) (int, error) {
	if s == "" {
		return 0, errBadCPUQuantity
	}
	if n, ok := strings.CutSuffix(s, "m"); ok {
		if !allDigits(n) {
			return 0, errBadCPUQuantity
		}
		m, err := strconv.Atoi(n)
		if err != nil || m <= 0 {
			return 0, errBadCPUQuantity
		}
		return m, nil
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if !allDigits(whole) || (hasFrac && (!allDigits(frac) || len(frac) > maxCPUDecimals)) {
		return 0, errBadCPUQuantity
	}
	w, err := strconv.Atoi(whole)
	if err != nil {
		return 0, errBadCPUQuantity
	}
	m := w * 1000
	if hasFrac {
		f, _ := strconv.Atoi((frac + "000")[:maxCPUDecimals])
		m += f
	}
	if m <= 0 {
		return 0, errBadCPUQuantity
	}
	return m, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
