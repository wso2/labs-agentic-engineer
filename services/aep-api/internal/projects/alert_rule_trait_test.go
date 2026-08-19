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

// The evaluation interval is the detection latency: an error waits at most one
// interval before it can raise an alert, so it must stay small. It is NOT the
// cooldown between RCA runs — that is the observer's ALERT_SUPPRESSION_WINDOW.
// Regressing this to the repair-loop cadence (it was once 30m) buys a cooldown
// that suppression already provides, at the cost of 30m of blindness.
func TestAutoRCARuleDetectsWithinAMinute(t *testing.T) {
	interval, err := time.ParseDuration(condition(t)["interval"].(string))
	if err != nil {
		t.Fatalf("interval is not a duration: %v", err)
	}
	if interval > time.Minute {
		t.Errorf("evaluation interval = %s, want <= 1m: an error waits this long to be detected", interval)
	}
}

// The adapter rejects sub-minute durations (whole minutes or hours only), so an
// interval below the floor makes every rule fail to sync rather than detect
// faster.
func TestAutoRCARuleIntervalIsAWholeMinute(t *testing.T) {
	cond := condition(t)
	for _, field := range []string{"interval", "window"} {
		d, err := time.ParseDuration(cond[field].(string))
		if err != nil {
			t.Fatalf("%s is not a duration: %v", field, err)
		}
		if d < time.Minute || d%time.Minute != 0 {
			t.Errorf("%s = %s, want a whole number of minutes >= 1m", field, d)
		}
	}
}

// The window must exceed the interval so consecutive evaluations overlap. Equal
// windows merely abut, which drops any line fluent-bit indexes just after a
// window closed. The resulting double-count is absorbed by suppression.
func TestAutoRCARuleWindowOverlapsTheInterval(t *testing.T) {
	cond := condition(t)
	window, err := time.ParseDuration(cond["window"].(string))
	if err != nil {
		t.Fatalf("window is not a duration: %v", err)
	}
	interval, err := time.ParseDuration(cond["interval"].(string))
	if err != nil {
		t.Fatalf("interval is not a duration: %v", err)
	}
	if window <= interval {
		t.Errorf("window %s <= interval %s: evaluations abut instead of overlapping, so a line indexed just after a window closes is never seen", window, interval)
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
