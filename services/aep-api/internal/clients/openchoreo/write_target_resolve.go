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
	"errors"
	"fmt"
	"time"
)

const (
	writeTargetRetryBudget = 2 * time.Minute
	writeTargetBackoffMin  = 1 * time.Second
	writeTargetBackoffMax  = 15 * time.Second
)

type retrySpec struct {
	budget     time.Duration
	initial    time.Duration
	maxBackoff time.Duration
	sleep      func(context.Context, time.Duration) error
	now        func() time.Time
}

func ResolvePlatformWriteTarget(ctx context.Context, cfg Config) (string, error) {
	c := newProjectCellClient(cfg)
	return resolveWriteTarget(ctx, func(ctx context.Context) (string, error) {
		p, err := c.getPipeline(ctx, PlatformPipelineNamespace, PlatformPipelineName)
		if err != nil {
			return "", err
		}
		return PipelineSourceEnvironment(p)
	}, retrySpec{
		budget:     writeTargetRetryBudget,
		initial:    writeTargetBackoffMin,
		maxBackoff: writeTargetBackoffMax,
		sleep:      sleepCtx,
		now:        time.Now,
	})
}

func resolveWriteTarget(ctx context.Context, fetch func(context.Context) (string, error), r retrySpec) (string, error) {
	if r.now == nil {
		r.now = time.Now
	}
	if r.sleep == nil {
		r.sleep = sleepCtx
	}
	if r.budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.budget)
		defer cancel()
	}
	start := r.now()
	backoff := r.initial
	var last error
	for {
		name, err := fetch(ctx)
		if err == nil {
			return name, nil
		}
		if errors.Is(err, ErrPipelineSourceAmbiguous) || errors.Is(err, ErrPipelineCyclic) {
			return "", err
		}
		last = err
		if r.now().Sub(start) >= r.budget || ctx.Err() != nil {
			return "", fmt.Errorf("resolve write-target from %s/%s: %w",
				PlatformPipelineNamespace, PlatformPipelineName, last)
		}
		if err := r.sleep(ctx, backoff); err != nil {
			return "", err
		}
		backoff *= 2
		if backoff > r.maxBackoff {
			backoff = r.maxBackoff
		}
	}
}
