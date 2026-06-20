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

package services

import (
	"errors"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	// ErrSpecNotFound / ErrDesignNotFound re-export the canonical sentinels now
	// owned by the artifacts feature (the artifact-not-found family), so the
	// task feature can reference them without importing the flat services pkg.
	ErrSpecNotFound      = artifacts.ErrSpecNotFound
	ErrSpecEmpty         = errors.New("spec content is empty")
	ErrSpecNotApproved   = errors.New("spec must be saved (tagged) before generating a design")
	ErrDesignNotFound    = artifacts.ErrDesignNotFound
	ErrDesignNotApproved = errors.New("design must be saved (tagged) before generating tasks")
	ErrTasksInFlight     = errors.New("tasks already in progress; cannot regenerate")
	ErrBuildNotFound     = errors.New("build not found")
	ErrDeploymentFailed  = errors.New("deployment failed")
)
