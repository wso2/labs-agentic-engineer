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

package run

import (
	"context"

	"go.temporal.io/sdk/interceptor"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// serviceIdentityInterceptor marks every activity's context as
// orchestration/async (auth.WithServiceIdentity) before the activity body
// runs. A Temporal activity executes on a worker goroutine, disconnected from
// (and often long after) whatever HTTP request originally started the run —
// it never legitimately carries a real inbound user JWT. Without this, an OC
// client using UserJWTStrategy would see no token in an activity's context
// and (by design) refuse the call rather than silently using the service
// identity, breaking every OC call an activity makes (build polling, agent
// dispatch, API-trait sync). Applying it once here, worker-wide, means no
// individual Activities method has to remember to call it itself.
type serviceIdentityInterceptor struct {
	interceptor.WorkerInterceptorBase
}

// InterceptActivity implements interceptor.WorkerInterceptor.
func (serviceIdentityInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &serviceIdentityActivityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
	}
}

type serviceIdentityActivityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
}

// ExecuteActivity implements interceptor.ActivityInboundInterceptor.
func (i *serviceIdentityActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	return i.Next.ExecuteActivity(auth.WithServiceIdentity(ctx), in)
}
