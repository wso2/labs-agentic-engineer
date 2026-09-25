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

package app

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/clients/thunderapp"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/projects"
)

// thunderApplicationReader adapts thunderapp.Client to projects.ThunderApplicationReader
// so the clients package stays free of the projects domain import.
type thunderApplicationReader struct {
	client *thunderapp.Client
}

func (r thunderApplicationReader) FindByResource(ctx context.Context, resourceName, environment string) (*projects.ThunderApplicationView, error) {
	if r.client == nil {
		return nil, nil
	}
	app, err := r.client.FindByResource(ctx, resourceName, environment)
	if err != nil {
		if thunderapp.IsNotFound(err) {
			return nil, projects.ErrThunderApplicationAPIMissing
		}
		return nil, err
	}
	if app == nil {
		return nil, nil
	}
	return &projects.ThunderApplicationView{
		RedirectURIs:       app.RedirectURIs,
		Ready:              app.Ready,
		Generation:         app.Generation,
		ObservedGeneration: app.ObservedGeneration,
	}, nil
}

// thunderWaitMarkerCatalog adapts dependencies.ResourceTypeCatalog onto the
// projects-owned consumerURLMarker vocabulary so projects never imports
// dependencies (domains import ports only).
type thunderWaitMarkerCatalog struct {
	cat *dependencies.ResourceTypeCatalog
}

func (c thunderWaitMarkerCatalog) MarkersByName(ctx context.Context) (map[string]projects.ConsumerURLMarker, error) {
	markers, err := c.cat.MarkersByName(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]projects.ConsumerURLMarker, len(markers))
	for name, m := range markers {
		out[name] = toConsumerURLMarker(m)
	}
	return out, nil
}

// toConsumerURLMarker projects dependencies.TypeMarkers onto projects'
// ConsumerURLMarker. When EnvConfig is set but Path is empty, fills the
// default callback path so the wait never joins an empty suffix.
func toConsumerURLMarker(m dependencies.TypeMarkers) projects.ConsumerURLMarker {
	path := m.ConsumerURLPath
	if m.ConsumerURLEnvConfig != "" && path == "" {
		path = dependencies.DefaultConsumerURLPath
	}
	return projects.ConsumerURLMarker{
		EnvConfig: m.ConsumerURLEnvConfig,
		Path:      path,
	}
}
