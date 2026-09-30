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

// Package sreagent converges the AE-owned SRE agent Secret on the observability
// plane.
package sreagent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/organization"
)

const HashAnnotation = "aep.wso2.com/sre-llm-hash"

type Desired struct {
	Configured bool
	Model      string
	BaseURL    string
	APIKey     string
	MCPToken   string
}

// DesiredFrom maps the effective SRE connection onto what the stock agent
// reads: RCA_MODEL_NAME must carry the `openai:` prefix for init_chat_model.
func DesiredFrom(e organization.EffectiveSRE, mcpToken string) Desired {
	if e.Source == organization.SRESourceNone {
		return Desired{}
	}
	return Desired{Configured: true, Model: "openai:" + e.Conn.Model, BaseURL: e.Conn.BaseURL, APIKey: e.Key, MCPToken: mcpToken}
}

// SecretData returns the secret data for the SRE agent. It always carries all
// four keys because the Deployment's secretKeyRefs require them.
func (d Desired) SecretData() map[string][]byte {
	return map[string][]byte{
		"RCA_LLM_API_KEY":  []byte(d.APIKey),
		"RCA_MODEL_NAME":   []byte(d.Model),
		"RCA_LLM_BASE_URL": []byte(d.BaseURL),
		"AEP_MCP_TOKEN":    []byte(d.MCPToken),
	}
}

// Replicas returns the desired number of replicas: 1 when Configured, else 0.
func (d Desired) Replicas() int32 {
	if d.Configured {
		return 1
	}
	return 0
}

// Hash returns the sha256 hex hash over the four values (Model, BaseURL, APIKey,
// MCPToken). This is used to detect configuration changes that require a restart.
func (d Desired) Hash() string {
	h := sha256.New()
	for _, v := range []string{d.Model, d.BaseURL, d.APIKey, d.MCPToken} {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

type DeploymentState struct {
	Replicas           int32
	UpdatedReplicas    int32
	AvailableReplicas  int32
	ObservedGeneration int64
	Generation         int64
	TemplateHash       string
}

type PodState struct {
	Hash             string
	WaitingReason    string
	TerminatedReason string
	ExitCode         int32
}

type Status string

const (
	StatusUnconfigured Status = "unconfigured"
	StatusApplying     Status = "applying"
	StatusRunning      Status = "running"
	StatusFailed       Status = "failed"
)

// StatusOf reads the console's SRE agent status off the Deployment and its
// pods: failed wins over applying, so a crashlooping new pod never reads as
// "still applying".
func StatusOf(d Desired, dep DeploymentState, pods []PodState) (Status, string) {
	if !d.Configured {
		return StatusUnconfigured, ""
	}
	want := d.Hash()
	for _, p := range pods {
		if p.Hash != want {
			continue
		}
		if p.WaitingReason == "CrashLoopBackOff" || p.TerminatedReason == "Error" {
			return StatusFailed, fmt.Sprintf("SRE agent exited (code %d, %s): the model or key was rejected at startup", p.ExitCode, p.WaitingReason+p.TerminatedReason)
		}
	}
	if dep.TemplateHash != want || dep.ObservedGeneration < dep.Generation ||
		dep.UpdatedReplicas < dep.Replicas || dep.AvailableReplicas < dep.Replicas {
		return StatusApplying, ""
	}
	return StatusRunning, ""
}
