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

import "gopkg.in/yaml.v3"

// AgentGuardrail is one `x-aep.guardrails` entry of an agent.afm.md: an
// AI-gateway policy the agent's model traffic should run through, with the
// use-case params and the reason it is there. What the platform adds on top —
// the policy version and the JSONPaths — is the governance slice's
// (agentgovernance/guardrails.go), never the document's.
type AgentGuardrail struct {
	Policy string
	Params map[string]any
	Why    string
}

// AgentGuardrailDeclarations is what an agent.afm.md says about guardrails.
type AgentGuardrailDeclarations struct {
	Guardrails []AgentGuardrail
	// Instructions is the prompt body, which every model request carries.
	Instructions string
	// Readable is false when the document could not be read at all, so what
	// it declares is unknown — which is not the same as declaring nothing.
	Readable bool
	// UsesTools and TakesFiles say whether the agent declares x-aep.tools or
	// x-aep.attachments: with either, the last message of a model call is not
	// always the user's text, which decides where a guardrail can read it.
	UsesTools  bool
	TakesFiles bool
}

// AgentGuardrails reads the declared guardrails out of an agent.afm.md.
//
// Lenient about entries, like parseAFMToolEntries: the write gate
// (agentfold/afmgate.go) has already guaranteed a committed document's shape,
// so an entry of the wrong shape is skipped rather than reported. Strict about
// the document: one that cannot be read says so, because a deploy that read
// it as "declares nothing" would remove every guardrail the agent has.
func AgentGuardrails(afm string) AgentGuardrailDeclarations {
	m := afmFrontMatterPattern.FindStringSubmatch(afm)
	if m == nil {
		return AgentGuardrailDeclarations{}
	}
	var front struct {
		XAep struct {
			Guardrails  []map[string]any `yaml:"guardrails"`
			Tools       any              `yaml:"tools"`
			Attachments any              `yaml:"attachments"`
		} `yaml:"x-aep"`
	}
	if err := yaml.Unmarshal([]byte(m[1]), &front); err != nil {
		return AgentGuardrailDeclarations{}
	}
	out := AgentGuardrailDeclarations{Instructions: m[2], Readable: true,
		UsesTools: front.XAep.Tools != nil, TakesFiles: front.XAep.Attachments != nil}
	for _, entry := range front.XAep.Guardrails {
		policy, _ := entry["policy"].(string)
		if policy == "" {
			continue
		}
		params, _ := entry["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
		}
		why, _ := entry["why"].(string)
		out.Guardrails = append(out.Guardrails, AgentGuardrail{Policy: policy, Params: params, Why: why})
	}
	return out
}
