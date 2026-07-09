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

package codingagent

import (
	"context"
	"errors"
	"testing"
)

// recordingObserver records each OnComponentDeployed call and can be scripted to
// fail, so the fan-out's error-isolation can be asserted order-independently.
type recordingObserver struct {
	name string
	err  error
	// log is a shared slice both observers append their name to, proving both ran
	// and preserving call order.
	log *[]string
	// ctxSeen captures the context passed through (nil until called).
	ctxSeen context.Context
	args    [3]string
}

func (o *recordingObserver) OnComponentDeployed(ctx context.Context, orgID, projectID, component string) error {
	*o.log = append(*o.log, o.name)
	o.ctxSeen = ctx
	o.args = [3]string{orgID, projectID, component}
	return o.err
}

func TestMultiDeployObserver_FansOutToAll(t *testing.T) {
	var log []string
	a := &recordingObserver{name: "a", log: &log}
	b := &recordingObserver{name: "b", log: &log}
	m := NewMultiDeployObserver(a, b)

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	if err := m.OnComponentDeployed(ctx, "acme", "widgets", "order-service"); err != nil {
		t.Fatalf("fan-out must return nil (best-effort), got %v", err)
	}
	if len(log) != 2 {
		t.Fatalf("both observers must run, got %v", log)
	}
	for _, o := range []*recordingObserver{a, b} {
		if o.args != [3]string{"acme", "widgets", "order-service"} {
			t.Errorf("observer %q got wrong args %v", o.name, o.args)
		}
		if o.ctxSeen == nil || o.ctxSeen.Value(ctxKey{}) != "v" {
			t.Errorf("observer %q did not receive the caller context", o.name)
		}
	}
}

func TestMultiDeployObserver_FirstErrorDoesNotBlockSecond(t *testing.T) {
	var log []string
	failing := &recordingObserver{name: "failing", log: &log, err: errors.New("grant failed")}
	ok := &recordingObserver{name: "ok", log: &log}
	// The failing observer is registered FIRST — its error must not stop the
	// second from running, and the aggregate still returns nil (best-effort).
	m := NewMultiDeployObserver(failing, ok)

	if err := m.OnComponentDeployed(context.Background(), "acme", "widgets", "order-service"); err != nil {
		t.Fatalf("a failing observer must not surface an error, got %v", err)
	}
	if len(log) != 2 || log[0] != "failing" || log[1] != "ok" {
		t.Fatalf("the second observer must run after the first fails, got %v", log)
	}
}

func TestMultiDeployObserver_DropsNilObservers(t *testing.T) {
	var log []string
	only := &recordingObserver{name: "only", log: &log}
	m := NewMultiDeployObserver(nil, only, nil)

	if err := m.OnComponentDeployed(context.Background(), "acme", "widgets", "c"); err != nil {
		t.Fatalf("OnComponentDeployed: %v", err)
	}
	if len(log) != 1 || log[0] != "only" {
		t.Fatalf("nil observers must be dropped, got %v", log)
	}
}
