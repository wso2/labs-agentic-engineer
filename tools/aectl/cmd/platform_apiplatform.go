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

package cmd

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/ui"
)

type apiPlatformDeps struct {
	isInstalled func(context.Context, *kubernetes.Clientset) (bool, error)
}

var defaultAPIPlatformDeps = apiPlatformDeps{
	isInstalled: isAPIPlatformInstalled,
}

// checkAPIPlatform verifies the WSO2 API Platform operator is installed on the
// cluster before proceeding with the AEP platform install. AEP services that
// use the api-configuration trait emit RestApi CRs; without the operator those
// CRs have no CRD and every deploy fails at the data-plane agent.
func checkAPIPlatform(ctx context.Context, client *kubernetes.Clientset) error {
	return runAPIPlatformCheck(ctx, client, defaultAPIPlatformDeps)
}

func runAPIPlatformCheck(ctx context.Context, client *kubernetes.Clientset, deps apiPlatformDeps) error {
	sp := ui.NewSpinner("Checking WSO2 API Platform")
	sp.Start()

	installed, err := deps.isInstalled(ctx, client)
	if err != nil {
		sp.Fail("WSO2 API Platform check failed")
		return err
	}
	if installed {
		sp.Success("WSO2 API Platform")
		return nil
	}
	sp.Fail("WSO2 API Platform not found")
	return fmt.Errorf("WSO2 API Platform is not installed on this cluster — install it and re-run 'aectl platform install'")
}

// isAPIPlatformInstalled checks whether the WSO2 API Platform operator CRDs are
// registered by querying the discovery API for the gateway.api-platform.wso2.com
// API group. It returns false (not an error) when the group is absent.
func isAPIPlatformInstalled(ctx context.Context, client *kubernetes.Clientset) (bool, error) {
	_, err := client.Discovery().ServerResourcesForGroupVersion("gateway.api-platform.wso2.com/v1alpha1")
	if err != nil {
		if isNotFoundDiscoveryError(err) {
			return false, nil
		}
		return false, fmt.Errorf("check API Platform CRDs: %w", err)
	}
	return true, nil
}

// isNotFoundDiscoveryError returns true when the discovery client error indicates
// the requested API group/version is not registered on the cluster. The k8s
// discovery client does not wrap these as apierrors.StatusError consistently
// across server versions, so we also check the message text.
func isNotFoundDiscoveryError(err error) bool {
	return apierrors.IsNotFound(err) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "404")
}
