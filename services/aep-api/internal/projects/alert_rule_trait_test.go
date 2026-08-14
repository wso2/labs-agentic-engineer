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
	"testing"
	"time"
)

// condition pulls the auto-RCA rule's condition block out of the single trait
// instance the platform provisions per component.
func condition(t *testing.T) map[string]interface{} {
	t.Helper()
	traits, _ := DesiredObservabilityAlertRuleTraits("api")
	if len(traits) != 1 {
		t.Fatalf("want 1 auto-RCA trait instance, got %d", len(traits))
	}
	cond, ok := traits[0].Parameters["condition"].(map[string]interface{})
	if !ok {
		t.Fatalf("condition missing or not a map: %#v", traits[0].Parameters["condition"])
	}
	return cond
}

// The evaluation interval is the floor on how often one component can drive an
// RCA run, and it is sized to the alert → RCA → issue → coding-agent → PR loop
// (~30m). A shorter interval re-analyses a failure that is already being fixed.
func TestAutoRCARuleEvaluatesAtTheRepairLoopCadence(t *testing.T) {
	got := condition(t)["interval"]
	if got != "30m" {
		t.Errorf("evaluation interval = %v, want 30m", got)
	}
}

// Window and interval must stay equal so consecutive evaluations tile the
// timeline: a shorter window leaves errors in the gap unobserved, a longer one
// re-counts lines an earlier evaluation already alerted on.
func TestAutoRCARuleWindowTilesTheInterval(t *testing.T) {
	cond := condition(t)
	window, err := time.ParseDuration(cond["window"].(string))
	if err != nil {
		t.Fatalf("window is not a duration: %v", err)
	}
	interval, err := time.ParseDuration(cond["interval"].(string))
	if err != nil {
		t.Fatalf("interval is not a duration: %v", err)
	}
	if window != interval {
		t.Errorf("window %s != interval %s: evaluations leave a gap or overlap", window, interval)
	}
}

// A single matching error line must still trip the rule — widening the window
// must not silently raise the bar for what counts as a failure.
func TestAutoRCARuleTripsOnASingleErrorLine(t *testing.T) {
	cond := condition(t)
	if op := cond["operator"]; op != "gte" {
		t.Errorf("operator = %v, want gte", op)
	}
	if th := cond["threshold"]; th != 1 {
		t.Errorf("threshold = %v, want 1", th)
	}
}
