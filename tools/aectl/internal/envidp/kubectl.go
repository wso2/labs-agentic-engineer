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

package envidp

import (
	"context"
	"fmt"
	"os/exec"
)

// execKubectl runs kubectl with the given args, forwarding kubeconfig if
// non-empty, and returns stdout. Used for the handful of operations this
// package needs against custom resources (annotating the Environment) that
// have no typed client here — the same pattern cmd/platform_gateway.go's
// execKubectl already uses for the same reason.
func execKubectl(ctx context.Context, kubeconfig string, args ...string) ([]byte, error) {
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w\n%s", err, ee.Stderr)
		}
		return nil, err
	}
	return out, nil
}
