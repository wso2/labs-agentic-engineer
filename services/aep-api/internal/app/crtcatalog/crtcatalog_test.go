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

package crtcatalog

import (
	"context"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/spec"
)

func TestCatalog_ProjectsTheRepoCatalogForDesignSave(t *testing.T) {
	t.Parallel()
	src := openchoreo.ResourceTypeDir("../../../../../deployments/single-cluster/resource-types")
	got, err := New(dependencies.NewResourceTypeCatalog(src, true)).ResourceTypesByName(context.Background())
	if err != nil {
		t.Fatalf("ResourceTypesByName: %v", err)
	}

	thunder := got["thunder-app"]
	want := spec.CRTType{
		EndUserAuth:          true,
		ConsumerURLEnvConfig: "redirectUris",
		ConsumerURLPath:      dependencies.DefaultConsumerURLPath,
		Skill:                "thunder-authentication",
		Description:          thunder.Description,
		Outputs:              []string{"client_id", "issuer", "jwks_url", "scopes", "resource"},
	}
	if thunder.Description == "" || !reflect.DeepEqual(thunder, want) {
		t.Errorf("thunder-app = %+v, want %+v (with a description)", thunder, want)
	}

	pg := got["postgres-cnpg"]
	if pg.EndUserAuth || pg.Skill != "" || pg.ConsumerURLEnvConfig != "" {
		t.Errorf("postgres-cnpg must carry no markers: %+v", pg)
	}
	if want := []string{"host", "port", "dbname", "user", "password"}; !reflect.DeepEqual(pg.Outputs, want) {
		t.Errorf("postgres-cnpg outputs = %v, want %v", pg.Outputs, want)
	}
}
