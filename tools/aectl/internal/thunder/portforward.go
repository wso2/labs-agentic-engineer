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

package thunder

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// remotePort is Thunder's admin service port inside the cluster.
const remotePort = "8090"

// PortForwardHandle is a running kubectl port-forward process.
type PortForwardHandle struct {
	// Port is the local TCP port bound by kubectl port-forward.
	Port string
	cmd  *exec.Cmd
	// stderr captures kubectl's error output for diagnostics.
	stderr bytes.Buffer
	// exited is closed (after exitErr is set) when the process exits.
	exited  chan struct{}
	exitErr error
}

// Stop kills the port-forward process and blocks until it exits, releasing all
// OS resources. Safe to call more than once.
func (h *PortForwardHandle) Stop() {
	_ = h.cmd.Process.Kill()
	<-h.exited
}

// PortForward starts kubectl port-forward to svc/thunder-service in the given
// namespace on an ephemeral local port. Caller must call Stop() when done.
func PortForward(ctx context.Context, namespace, kubeconfig string) (*PortForwardHandle, error) {
	port, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("pick local port for Thunder port-forward: %w", err)
	}

	args := []string{
		"port-forward",
		"-n", namespace,
		"svc/thunder-service",
		port + ":" + remotePort,
	}
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}

	h := &PortForwardHandle{
		Port:   port,
		exited: make(chan struct{}),
	}
	h.cmd = exec.CommandContext(ctx, "kubectl", args...)
	h.cmd.Stderr = &h.stderr

	if err := h.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start port-forward to Thunder: %w", err)
	}

	// Reap the process in the background so cmd.Wait() is always called.
	go func() {
		h.exitErr = h.cmd.Wait()
		close(h.exited)
	}()

	return h, nil
}

// WaitForReachable retries GET /oauth2/jwks until Thunder responds (any HTTP
// status) or timeout expires. It returns early if the port-forward process
// exits before Thunder becomes reachable, including any captured stderr output.
func WaitForReachable(ctx context.Context, baseURL string, timeout time.Duration, pf *PortForwardHandle) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		resp, err := client.Get(baseURL + "/oauth2/jwks")
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("thunder at %s not reachable after %s: %w", baseURL, timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pf.exited:
			msg := strings.TrimSpace(pf.stderr.String())
			if msg != "" {
				return fmt.Errorf("kubectl port-forward exited before Thunder became reachable (stderr: %s): %w", msg, pf.exitErr)
			}
			return fmt.Errorf("kubectl port-forward exited before Thunder became reachable: %w", pf.exitErr)
		case <-time.After(2 * time.Second):
		}
	}
}

// pickFreePort asks the OS for an available TCP port by binding to :0.
func pickFreePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return strconv.Itoa(port), nil
}
