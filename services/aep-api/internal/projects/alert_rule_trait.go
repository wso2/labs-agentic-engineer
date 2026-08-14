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

package projects

import (
	"fmt"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// autoRCALogQuery is the default log phrase the platform watches for on every
// service component. A SINGLE instance suffices: the logs adapter compiles a
// rule's `query` into a case-INSENSITIVE `wildcard: *<phrase>*` (it sets
// case_insensitive: true), so "error" already matches ERROR, Error, etc.
//
// This used to file two rules ("error" and "ERROR") to work around a
// case-SENSITIVE adapter. Now that the adapter is case-insensitive both rules
// match the identical set and BOTH fire on every error line — two incidents,
// so two duplicate RCA reports per error. Collapsing to one rule removes that
// double-trigger while (case-insensitively) still catching every case form.
const autoRCALogQuery = "error"

// autoRCADefaultChannel is the notification channel stamped on auto-provisioned
// rules. The trait requires ≥1 channel (incident-only rules are rejected);
// "default" is a placeholder the observer accepts when no real channel exists.
const autoRCADefaultChannel = "default"

// autoRCAEvaluationInterval is how often the adapter evaluates the rule, and so
// the floor on the gap between two RCA runs for one component. It is sized to
// the downstream repair loop: alert → RCA → GitHub issue → coding-agent → PR
// takes ~30m, and re-firing inside that window analyses a failure that is
// already being fixed.
//
// autoRCAEvaluationWindow is the lookback each evaluation aggregates over, and
// is held EQUAL to the interval so consecutive evaluations tile the timeline.
// A window shorter than the interval leaves an unobserved gap between runs
// (a 30m interval over a 5m window would miss errors logged in the other 25m);
// a longer one re-counts lines an earlier evaluation already alerted on.
const (
	autoRCAEvaluationWindow   = "30m"
	autoRCAEvaluationInterval = "30m"
)

// DesiredObservabilityAlertRuleTraits returns the default "error → RCA"
// observability-alert-rule trait instance (+ its per-environment config) for a
// component. componentName is the k8s-shaped name; the instance is named
// `<componentName>-auto-rca-error`.
//
// The split mirrors the api-configuration trait: rule shape (source/query,
// condition) lives in the component-level trait Parameters; the incident /
// triggerAiRca action lives in the per-environment config (the trait template
// reads triggerAiRca from environmentConfigs). incident.enabled is set true
// because the trait validation requires it whenever triggerAiRca is true.
func DesiredObservabilityAlertRuleTraits(componentName string) (traits []openchoreo.ComponentTrait, configs map[string]map[string]interface{}) {
	inst := componentName + "-auto-rca-error"
	traits = []openchoreo.ComponentTrait{{
		InstanceName: inst,
		Kind:         "ClusterTrait",
		Name:         "observability-alert-rule",
		Parameters: map[string]interface{}{
			"description": fmt.Sprintf(
				"Auto-provisioned: trigger RCA when %s logs a line containing %q (case-insensitive).",
				componentName, autoRCALogQuery),
			"severity": "critical",
			"source": map[string]interface{}{
				"type":  "log",
				"query": autoRCALogQuery,
			},
			"condition": map[string]interface{}{
				"window":    autoRCAEvaluationWindow,
				"interval":  autoRCAEvaluationInterval,
				"operator":  "gte",
				"threshold": 1,
			},
		},
	}}
	configs = map[string]map[string]interface{}{
		inst: {
			"enabled": true,
			"actions": map[string]interface{}{
				"notifications": map[string]interface{}{
					"channels": []interface{}{autoRCADefaultChannel},
				},
				"incident": map[string]interface{}{
					"enabled":      true,
					"triggerAiRca": true,
				},
			},
		},
	}
	return traits, configs
}
