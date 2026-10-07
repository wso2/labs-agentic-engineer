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

package spec

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentToolText is the text an ai-agent's tool definitions are built from:
// for each operation its `x-aep.tools.openapi[].allow` names, the operation's
// id, summary and description, and its parameters' names and descriptions,
// read from the provider component's contract in the design. A tool-using
// agent sends its tool definitions with every model request, so this is part
// of what a whole-request gateway check reads on every call.
//
// Lenient like parseAFMToolEntries: an unreadable document, a provider with no
// contract in the design, or an allowed operation the contract lacks
// contributes nothing rather than failing. Wording the agent adds in code (a
// parameter's describe() text) is not in the contract and so not here.
func AgentToolText(afm string, components []DesignComponent) []string {
	fm, err := parseAFMToolEntries(afm)
	if err != nil || len(fm.Tools) == 0 {
		return nil
	}
	contracts := make(map[string]string, len(components))
	for i := range components {
		contracts[components[i].Name] = components[i].OpenAPISpec
	}
	var out []string
	for _, tool := range fm.Tools {
		allowed := make(map[string]bool, len(tool.Allow))
		for _, op := range tool.Allow {
			allowed[op] = true
		}
		out = append(out, allowedOperationText(contracts[tool.Component], allowed)...)
	}
	return out
}

// allowedOperationText walks one contract's operations and returns the text of
// those whose operationId is allowed.
func allowedOperationText(contract string, allowed map[string]bool) []string {
	if strings.TrimSpace(contract) == "" || len(allowed) == 0 {
		return nil
	}
	var raw any
	if err := yaml.Unmarshal([]byte(contract), &raw); err != nil {
		return nil
	}
	root, _ := raw.(map[string]any)
	paths, _ := root["paths"].(map[string]any)
	var out []string
	add := func(v any) {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	for _, itemRaw := range paths {
		item, _ := itemRaw.(map[string]any)
		for method, opRaw := range item {
			if !openAPIHTTPMethods[strings.ToLower(method)] {
				continue
			}
			op, _ := opRaw.(map[string]any)
			id, _ := op["operationId"].(string)
			if !allowed[id] {
				continue
			}
			add(id)
			add(op["summary"])
			add(op["description"])
			params, _ := op["parameters"].([]any)
			for _, pRaw := range params {
				p, _ := pRaw.(map[string]any)
				add(p["name"])
				add(p["description"])
			}
		}
	}
	return out
}
