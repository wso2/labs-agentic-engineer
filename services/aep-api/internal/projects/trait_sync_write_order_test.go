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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// UNIT tier for the ORDER of the two trait writes. The two halves of trait state
// live on different objects (Component.spec.traits, ReleaseBinding.spec
// .traitEnvironmentConfigs) and either write can fail alone, so the order
// decides what a half-applied sync leaves behind. A trait attached WITHOUT its
// per-environment config is not a partial success: it fails the whole binding's
// render, which is how one dropped write took down an entire project's
// deployment —
//
//	Failed to render resources: trait observability-alert-rule/…-auto-rca-error
//	validation failed: A notification channel is mandatory for alert rules

// orderRecordingOC records the sequence of trait writes so a test can assert
// which object was written first. `configWrites` captures each env-config call's
// payload in order.
type orderRecordingOC struct {
	*mocks.ComponentClientMock
	order        []string
	configWrites []map[string]map[string]interface{}
}

func newOrderRecordingOC() *orderRecordingOC {
	rec := &orderRecordingOC{ComponentClientMock: &mocks.ComponentClientMock{}}
	rec.ListDeploymentsFunc = nil
	rec.UpdateComponentTraitsFunc = func(context.Context, string, string, string, []openchoreo.ComponentTrait) error {
		rec.order = append(rec.order, "traits")
		return nil
	}
	rec.UpdateComponentTraitEnvironmentConfigsFunc = func(_ context.Context, _, _, _ string, configs map[string]map[string]interface{}) error {
		rec.order = append(rec.order, "configs")
		rec.configWrites = append(rec.configWrites, configs)
		return nil
	}
	return rec
}

// TestSyncComponentTraits_ConfigBeforeTraitShape — a protected component's
// jwtAuth + auto-RCA config must be on the binding BEFORE the traits that
// require it are attached to the Component.
func TestSyncComponentTraits_ConfigBeforeTraitShape(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
	}
	oc := newOrderRecordingOC()
	svc := NewTraitSyncService(oc, traitStoreWith(files))

	if err := svc.SyncComponentTraits(context.Background(), "acme", "proj", "api"); err != nil {
		t.Fatalf("SyncComponentTraits: %v", err)
	}
	if len(oc.order) != 2 {
		t.Fatalf("want exactly 2 writes for an enabled component, got %v", oc.order)
	}
	if oc.order[0] != "configs" || oc.order[1] != "traits" {
		t.Errorf("upserts must precede the trait shape; got %v", oc.order)
	}
	// The one config write must carry BOTH traits' configs — they share the
	// write, so splitting them would reintroduce the stranded-trait window.
	cfg := oc.configWrites[0]
	if _, ok := cfg["api-http"]; !ok {
		t.Errorf("api-configuration config missing from the upsert; got %v", keysOfAny(cfg))
	}
	if _, ok := cfg["api-auto-rca-error"]; !ok {
		t.Errorf("auto-RCA config missing from the upsert; got %v", keysOfAny(cfg))
	}
}

// TestSyncComponentTraits_TraitShapeBeforeTombstone — going the other way, the
// instance must leave the Component before its config is stripped, so a config
// is never pulled out from under an attached trait.
func TestSyncComponentTraits_TraitShapeBeforeTombstone(t *testing.T) {
	t.Parallel()
	// A web-application: no exposesAPI (api-configuration tombstoned) and not a
	// service (auto-RCA does not apply), so nothing wants a config and the only
	// config write is the removal.
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/web/design.json": webAppMd("web"),
	}
	oc := newOrderRecordingOC()
	svc := NewTraitSyncService(oc, traitStoreWith(files))

	if err := svc.SyncComponentTraits(context.Background(), "acme", "proj", "web"); err != nil {
		t.Fatalf("SyncComponentTraits: %v", err)
	}
	if len(oc.order) != 2 {
		t.Fatalf("want 2 writes (trait shape then tombstone), got %v", oc.order)
	}
	if oc.order[0] != "traits" || oc.order[1] != "configs" {
		t.Errorf("tombstones must follow the trait shape; got %v", oc.order)
	}
	if got := oc.configWrites[0]["web-http"]; got != nil {
		t.Errorf("want a nil tombstone for web-http, got %#v", got)
	}
}

// TestSyncComponentTraits_ConfigFailureLeavesTraitUnattached — the invariant the
// ordering buys: when the config write fails, the sync aborts BEFORE attaching
// the trait, so the binding stays renderable and the next sweep can converge.
// Previously the trait went on first and a failed config write left the whole
// ReleaseBinding permanently unrenderable.
func TestSyncComponentTraits_ConfigFailureLeavesTraitUnattached(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
	}
	oc := newOrderRecordingOC()
	oc.UpdateComponentTraitEnvironmentConfigsFunc = func(context.Context, string, string, string, map[string]map[string]interface{}) error {
		return errors.New("oc: conflict surfaced as 500")
	}
	svc := NewTraitSyncService(oc, traitStoreWith(files))

	err := svc.SyncComponentTraits(context.Background(), "acme", "proj", "api")
	if err == nil {
		t.Fatal("want the config-write failure to surface")
	}
	if !strings.Contains(err.Error(), "update trait env configs") {
		t.Errorf("error should name the failing write: %v", err)
	}
	if n := len(oc.UpdateComponentTraitsCalls()); n != 0 {
		t.Errorf("trait shape must NOT be attached when its config failed to land; got %d calls", n)
	}
}

// TestSplitTraitConfigs — the partition the write order is built on.
func TestSplitTraitConfigs(t *testing.T) {
	t.Parallel()

	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		up, rm := splitTraitConfigs(map[string]map[string]interface{}{
			"keep":  {"jwtAuth": map[string]interface{}{"enabled": true}},
			"drop":  nil,
			"empty": {},
		})
		if len(up) != 1 || up["keep"] == nil {
			t.Errorf("upserts = %v, want just 'keep'", keysOfAny(up))
		}
		if len(rm) != 2 {
			t.Errorf("removals = %v, want 'drop' and 'empty'", keysOfAny(rm))
		}
		if rm["drop"] != nil || rm["empty"] != nil {
			t.Error("removals must map to nil so the client deletes the key")
		}
	})

	t.Run("nil input yields two nil maps so both phases are skipped", func(t *testing.T) {
		t.Parallel()
		up, rm := splitTraitConfigs(nil)
		if up != nil || rm != nil {
			t.Errorf("want both nil, got upserts=%v removals=%v", up, rm)
		}
	})
}
