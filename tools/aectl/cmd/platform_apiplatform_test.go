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
	"errors"
	"testing"

	"k8s.io/client-go/kubernetes"
)

func TestRunAPIPlatformCheck_AlreadyInstalled(t *testing.T) {
	deps := apiPlatformDeps{
		isInstalled: func(context.Context, *kubernetes.Clientset) (bool, error) {
			return true, nil
		},
	}
	if err := runAPIPlatformCheck(context.Background(), nil, deps); err != nil {
		t.Fatalf("expected nil when API Platform is installed, got %v", err)
	}
}

func TestRunAPIPlatformCheck_NotInstalled(t *testing.T) {
	deps := apiPlatformDeps{
		isInstalled: func(context.Context, *kubernetes.Clientset) (bool, error) {
			return false, nil
		},
	}
	if err := runAPIPlatformCheck(context.Background(), nil, deps); err == nil {
		t.Fatal("expected error when API Platform is not installed, got nil")
	}
}

func TestRunAPIPlatformCheck_DetectionError(t *testing.T) {
	deps := apiPlatformDeps{
		isInstalled: func(context.Context, *kubernetes.Clientset) (bool, error) {
			return false, errors.New("api server unreachable")
		},
	}
	if err := runAPIPlatformCheck(context.Background(), nil, deps); err == nil {
		t.Fatal("expected error when detection fails, got nil")
	}
}
