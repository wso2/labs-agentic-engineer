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
	"errors"
	"fmt"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
)

func TestAggregateProvisionFailures_PreservesPermanentSentinel(t *testing.T) {
	t.Parallel()
	err := aggregateProvisionFailures([]provisioning.ProvisionFailure{{
		Dependency: "parcel-receipts",
		Reason:     "resources: resource \"x\" produced no new ResourceRelease within 1m0s",
		Err:        fmt.Errorf("%w: timed out", dependencies.ErrProvisionPermanent),
	}})
	if !errors.Is(err, delivery.ErrProvisionPermanent) {
		t.Fatalf("delivery.ErrProvisionPermanent must survive aggregation, got %v", err)
	}
	if !errors.Is(err, dependencies.ErrProvisionPermanent) {
		t.Fatalf("dependencies.ErrProvisionPermanent must survive aggregation, got %v", err)
	}
}

func TestAggregateProvisionFailures_BlipIsNotPermanent(t *testing.T) {
	t.Parallel()
	err := aggregateProvisionFailures([]provisioning.ProvisionFailure{{
		Dependency: "orders-db",
		Reason:     "connection refused",
		Err:        errors.New("connection refused"),
	}})
	if errors.Is(err, delivery.ErrProvisionPermanent) {
		t.Fatalf("a blip must not be marked permanent: %v", err)
	}
}
