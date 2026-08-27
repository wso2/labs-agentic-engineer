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

package openchoreo

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// EnvironmentClient lists OpenChoreo Environment names in an org namespace.
// ListNames is the provisioning.EnvironmentLister surface.
type EnvironmentClient interface {
	ListNames(ctx context.Context, orgID string) ([]string, error)
}

type environmentClient struct {
	oc *ocgen.ClientWithResponses
}

// NewEnvironmentClient builds the Environment list wrapper over the shared OC
// transport. Empty orgID returns an empty slice and does not call OC.
func NewEnvironmentClient(cfg Config) EnvironmentClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo environment client: %w", err))
	}
	return &environmentClient{oc: oc}
}

func (c *environmentClient) ListNames(ctx context.Context, orgID string) ([]string, error) {
	if strings.TrimSpace(orgID) == "" {
		return []string{}, nil
	}
	resp, err := c.oc.ListEnvironmentsWithResponse(ctx, orgID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list environments: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		})
	}
	names := make([]string, 0, len(resp.JSON200.Items))
	for _, item := range resp.JSON200.Items {
		names = append(names, item.Metadata.Name)
	}
	return names, nil
}
