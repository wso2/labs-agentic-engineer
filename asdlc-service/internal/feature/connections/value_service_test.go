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

package connections

import (
	"context"
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/models"
)

type fakeLookup struct{ conn *models.Connection }

func (f *fakeLookup) Get(_ context.Context, _, _ string) (*models.Connection, error) {
	return f.conn, nil
}

type fakeTaskStore struct {
	tasks   []models.ComponentTask
	updated *models.ComponentTask
}

func (f *fakeTaskStore) ListByProjectID(_ context.Context, _, _ string) ([]models.ComponentTask, error) {
	return f.tasks, nil
}
func (f *fakeTaskStore) Update(_ context.Context, t *models.ComponentTask) error {
	f.updated = t
	return nil
}

func TestSaveValues_ProvisionsCompletesAndCascades(t *testing.T) {
	conn := &models.Connection{
		Name: "openweather", ResourceTypeName: "openweather",
		ConfigSchema: []models.ConfigKey{
			{Key: "OPENWEATHER_BASE_URL", Secret: false},
			{Key: "OPENWEATHER_API_KEY", Secret: true},
		},
	}
	rc := &fakeRC{latest: "rel-1"}
	prov := &Provisioner{rc: rc, sm: &fakeSW{}, pollInterval: time.Millisecond, pollTimeout: time.Second}
	store := &fakeTaskStore{tasks: []models.ComponentTask{
		{Type: models.TaskTypeConfigCollection, ConnectionName: "openweather", Status: string(models.TaskStatusPending)},
		{Type: models.TaskTypeComponent, ComponentName: "weather-api", Status: string(models.TaskStatusOnHold)},
	}}
	var redispatched bool
	vs := &ValueService{
		lookup: &fakeLookup{conn: conn}, prov: prov, tasks: store,
		redispatch: func(_ context.Context, _, _ string) error { redispatched = true; return nil },
	}

	err := vs.SaveValues(context.Background(), "default", "weatherproj", "openweather",
		map[string]map[string]string{
			"development": {"OPENWEATHER_BASE_URL": "https://api.openweathermap.org", "OPENWEATHER_API_KEY": "k"},
		})
	if err != nil {
		t.Fatalf("SaveValues: %v", err)
	}
	// Provisioned: one pinned binding authored.
	if len(rc.bindings) != 1 || rc.bindings[0].Spec.ResourceRelease != "rel-1" {
		t.Fatalf("connection not provisioned: %+v", rc.bindings)
	}
	// config-collection task marked deployed.
	if store.updated == nil || store.updated.Status != string(models.TaskStatusDeployed) {
		t.Errorf("config-collection task not completed: %+v", store.updated)
	}
	// Cascade fired.
	if !redispatched {
		t.Error("redispatch not called")
	}
}

func TestSplitBySchema(t *testing.T) {
	schema := []models.ConfigKey{{Key: "URL"}, {Key: "TOKEN", Secret: true}}
	ev := splitBySchema(schema, map[string]string{"URL": "u", "TOKEN": "t", "UNKNOWN": "x"})
	if ev.Plain["URL"] != "u" || ev.Plain["UNKNOWN"] != "x" {
		t.Errorf("plain wrong: %+v", ev.Plain)
	}
	if ev.Secret["TOKEN"] != "t" {
		t.Errorf("secret wrong: %+v", ev.Secret)
	}
	if _, ok := ev.Secret["URL"]; ok {
		t.Error("URL should not be secret")
	}
}
