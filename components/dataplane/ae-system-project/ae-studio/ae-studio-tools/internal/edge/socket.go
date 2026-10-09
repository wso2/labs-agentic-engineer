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
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
)

// socketMode lets the pod's shared group (fsGroup) connect to a socket: the
// Files socket (ae-collab) and the MCP socket (ae-design-agent). The mount is
// the gate: each socket's emptyDir is mounted into its caller and this
// container only.
const socketMode fs.FileMode = 0o660

// ListenSocket binds a Unix socket at path. A socket file left by a previous
// run is removed first; any other file at path is an error, never removed.
// The socket is made 0660; closing the listener unlinks it.
func ListenSocket(path string) (net.Listener, error) {
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("socket %s: listen: %w", path, err)
	}
	if err := os.Chmod(path, socketMode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("socket %s: chmod: %w", path, err)
	}
	return ln, nil
}

// removeStaleSocket removes a socket file at path; nothing there is fine.
func removeStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("socket %s: stat: %w", path, err)
	}
	if fi.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("socket %s: exists and is not a socket", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("socket %s: remove stale socket: %w", path, err)
	}
	return nil
}
