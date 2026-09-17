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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveWriteTarget_SucceedsFirstTry(t *testing.T) {
	t.Parallel()
	var sleeps int
	got, err := resolveWriteTarget(context.Background(), func(context.Context) (string, error) {
		return "development", nil
	}, retrySpec{
		budget:      time.Minute,
		initial:     time.Second,
		maxBackoff:  15 * time.Second,
		sleep: func(context.Context, time.Duration) error { sleeps++; return nil },
	})
	if err != nil || got != "development" || sleeps != 0 {
		t.Fatalf("got %q err=%v sleeps=%d", got, err, sleeps)
	}
}

func TestResolveWriteTarget_RetriesFetchThenSucceeds(t *testing.T) {
	t.Parallel()
	var n int
	var delays []time.Duration
	got, err := resolveWriteTarget(context.Background(), func(context.Context) (string, error) {
		n++
		if n < 3 {
			return "", errors.New("connection refused")
		}
		return "default", nil
	}, retrySpec{
		budget:     time.Minute,
		initial:    time.Second,
		maxBackoff: 15 * time.Second,
		sleep: func(_ context.Context, d time.Duration) error {
			delays = append(delays, d)
			return nil
		},
	})
	if err != nil || got != "default" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if len(delays) != 2 || delays[0] != time.Second || delays[1] != 2*time.Second {
		t.Fatalf("delays %v, want 1s then 2s", delays)
	}
}

func TestResolveWriteTarget_RetriesEmptyPipeline(t *testing.T) {
	t.Parallel()
	var n int
	got, err := resolveWriteTarget(context.Background(), func(context.Context) (string, error) {
		n++
		if n == 1 {
			return "", fmt.Errorf("x: %w", ErrPipelineEmpty)
		}
		return "default", nil
	}, retrySpec{
		budget:     time.Minute,
		initial:    time.Second,
		maxBackoff: 15 * time.Second,
		sleep:      func(context.Context, time.Duration) error { return nil },
	})
	if err != nil || got != "default" || n != 2 {
		t.Fatalf("got %q err=%v n=%d", got, err, n)
	}
}

func TestResolveWriteTarget_BudgetCancelsFetch(t *testing.T) {
	t.Parallel()
	start := time.Now()
	_, err := resolveWriteTarget(context.Background(), func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}, retrySpec{
		budget:     50 * time.Millisecond,
		initial:    time.Hour,
		maxBackoff: time.Hour,
		sleep: func(context.Context, time.Duration) error {
			t.Fatal("slept after deadline")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "default/default") {
		t.Fatalf("err=%v, want it to name the pipeline after budget", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %s, deadline did not bound fetch", time.Since(start))
	}
}

func TestResolveWriteTarget_CyclicDoesNotRetry(t *testing.T) {
	t.Parallel()
	var n int
	_, err := resolveWriteTarget(context.Background(), func(context.Context) (string, error) {
		n++
		return "", fmt.Errorf("x: %w", ErrPipelineCyclic)
	}, retrySpec{
		budget:     time.Minute,
		initial:    time.Second,
		maxBackoff: 15 * time.Second,
		sleep:      func(context.Context, time.Duration) error { t.Fatal("slept"); return nil },
	})
	if !errors.Is(err, ErrPipelineCyclic) || n != 1 {
		t.Fatalf("err=%v n=%d", err, n)
	}
}

func TestResolveWriteTarget_AmbiguousDoesNotRetry(t *testing.T) {
	t.Parallel()
	var n int
	_, err := resolveWriteTarget(context.Background(), func(context.Context) (string, error) {
		n++
		return "", fmt.Errorf("x: %w", ErrPipelineSourceAmbiguous)
	}, retrySpec{
		budget:     time.Minute,
		initial:    time.Second,
		maxBackoff: 15 * time.Second,
		sleep:      func(context.Context, time.Duration) error { t.Fatal("slept"); return nil },
	})
	if !errors.Is(err, ErrPipelineSourceAmbiguous) || n != 1 {
		t.Fatalf("err=%v n=%d", err, n)
	}
}

func TestResolveWriteTarget_ExhaustsBudget(t *testing.T) {
	t.Parallel()
	_, err := resolveWriteTarget(context.Background(), func(context.Context) (string, error) {
		return "", errors.New("never")
	}, retrySpec{
		budget:     5 * time.Second,
		initial:    time.Second,
		maxBackoff: time.Second,
		sleep:      func(context.Context, time.Duration) error { return nil },
		now: func() func() time.Time {
			t0 := time.Unix(0, 0)
			n := 0
			return func() time.Time {
				n++
				return t0.Add(time.Duration(n) * 2 * time.Second)
			}
		}(),
	})
	if err == nil || !strings.Contains(err.Error(), "default/default") {
		t.Fatalf("err=%v, want it to name the pipeline after budget", err)
	}
}

func TestGetPipeline_DecodesPromotionPaths(t *testing.T) {
	t.Parallel()
	const wantSource = "development"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		wantPath := nsBase(PlatformPipelineNamespace) + "/deploymentpipelines/" + PlatformPipelineName
		if r.URL.Path != wantPath {
			t.Errorf("path %q want %q", r.URL.Path, wantPath)
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		p := pipelineLinear(wantSource, "staging")
		writeJSON(t, w, http.StatusOK, p)
	}))
	defer srv.Close()

	c := newProjectCellClient(Config{BaseURL: srv.URL})
	p, err := c.getPipeline(context.Background(), PlatformPipelineNamespace, PlatformPipelineName)
	if err != nil {
		t.Fatalf("getPipeline: %v", err)
	}
	got, err := PipelineSourceEnvironment(p)
	if err != nil || got != wantSource {
		t.Fatalf("PipelineSourceEnvironment: %q err=%v", got, err)
	}
}
