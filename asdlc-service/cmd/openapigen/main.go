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

// Command openapigen writes the BFF's code-first OpenAPI document to disk. It is
// the `make openapi` generator and the source of truth for the checked-in
// api/openapi.yaml that the console client + drift guard consume.
package main

import (
	"fmt"
	"os"

	"github.com/wso2/asdlc/asdlc-service/api"
)

func main() {
	out := "api/openapi.yaml"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	b, err := api.GenerateOpenAPIYAML()
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate openapi:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil { //nolint:gosec
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", out, len(b))
}
