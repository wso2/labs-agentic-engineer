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
	"time"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// RuntimeClient reads what a ReleaseBinding actually rendered into the
// dataplane: the resource tree, the child Pod, its log and its events.
//
// It exists because a coding-agent cycle is a `batch/v1 Job`, and OpenChoreo
// registers NO health check for that kind — a ReleaseBinding therefore reports
// Ready/"completed successfully" while its Job is still running or has already
// failed. The only truthful source is the Pod in the tree, so every caller in
// this codebase classifies from RuntimePod and NOTHING classifies from
// ReleaseBinding conditions. Do not "simplify" this back.
type RuntimeClient interface {
	// ReleaseBindingName resolves the component's binding in one environment.
	// Wrapped ErrNotFound means the Component (or its binding) is gone —
	// deleted by retention or by a cancel — which callers treat as a fact, not
	// a failure.
	ReleaseBindingName(ctx context.Context, orgName, projectName, componentName, environment string) (string, error)

	// PodSnapshot returns the newest Pod node in the binding's resource tree.
	// A tree with no Pod yet returns RuntimePod{Found:false} and a nil error:
	// "not scheduled yet" is an ordinary state on the way to Running.
	PodSnapshot(ctx context.Context, orgName, releaseBindingName string) (RuntimePod, error)

	// PodLogs reads the pod's log. sinceSeconds <= 0 reads what the platform
	// still holds for the pod.
	PodLogs(ctx context.Context, orgName, releaseBindingName, podName string, sinceSeconds int64) ([]PodLogLine, error)

	// PodEvents reads the pod's Kubernetes events — the only account a pod that
	// never started can give of itself.
	PodEvents(ctx context.Context, orgName, releaseBindingName, podName string) ([]RuntimeEvent, error)
}

type runtimeClient struct {
	oc *ocgen.ClientWithResponses
}

// NewRuntimeClient builds the runtime reader over the shared OC transport.
func NewRuntimeClient(cfg Config) RuntimeClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo runtime client: %w", err))
	}
	return &runtimeClient{oc: oc}
}

func (c *runtimeClient) ReleaseBindingName(ctx context.Context, orgName, projectName, componentName, environment string) (string, error) {
	componentQ := ocgen.ComponentQueryParam(ScopedComponentName(projectName, componentName))
	resp, err := c.oc.ListReleaseBindingsWithResponse(ctx, orgName, &ocgen.ListReleaseBindingsParams{
		Component: &componentQ,
	})
	if err != nil {
		return "", fmt.Errorf("openchoreo: list release bindings for %s: %w", componentName, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return "", handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		})
	}
	for _, rb := range resp.JSON200.Items {
		if rb.Spec == nil || rb.Spec.Environment != environment {
			continue
		}
		return rb.Metadata.Name, nil
	}
	return "", fmt.Errorf("%w: no %s release binding for component %s", ErrNotFound, environment, componentName)
}

func (c *runtimeClient) PodSnapshot(ctx context.Context, orgName, releaseBindingName string) (RuntimePod, error) {
	resp, err := c.oc.GetReleaseBindingK8sResourceTreeWithResponse(ctx, orgName,
		ocgen.ReleaseBindingNameParam(releaseBindingName))
	if err != nil {
		return RuntimePod{}, fmt.Errorf("openchoreo: resource tree for %s: %w", releaseBindingName, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return RuntimePod{}, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	var newest RuntimePod
	var newestAt time.Time
	for _, release := range resp.JSON200.RenderedReleases {
		for i := range release.Nodes {
			node := &release.Nodes[i]
			if node.Kind != "Pod" {
				continue
			}
			// A Job with backoffLimit 0 has one pod, but a re-render can leave a
			// stale one behind; the newest is the attempt in flight.
			at := time.Time{}
			if node.CreatedAt != nil {
				at = *node.CreatedAt
			}
			if newest.Found && !at.After(newestAt) {
				continue
			}
			newest, newestAt = PodFromNodeObject(node.Object, node.Name), at
		}
	}
	return newest, nil
}

func (c *runtimeClient) PodLogs(ctx context.Context, orgName, releaseBindingName, podName string, sinceSeconds int64) ([]PodLogLine, error) {
	params := &ocgen.GetReleaseBindingK8sResourceLogsParams{PodName: podName}
	if sinceSeconds > 0 {
		params.SinceSeconds = &sinceSeconds
	}
	resp, err := c.oc.GetReleaseBindingK8sResourceLogsWithResponse(ctx, orgName,
		ocgen.ReleaseBindingNameParam(releaseBindingName), params)
	if err != nil {
		return nil, fmt.Errorf("openchoreo: pod logs for %s: %w", podName, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	out := make([]PodLogLine, 0, len(resp.JSON200.LogEntries))
	for _, e := range resp.JSON200.LogEntries {
		out = append(out, PodLogLine{Timestamp: e.Timestamp.UTC(), Log: e.Log})
	}
	return out, nil
}

func (c *runtimeClient) PodEvents(ctx context.Context, orgName, releaseBindingName, podName string) ([]RuntimeEvent, error) {
	group := ""
	resp, err := c.oc.GetReleaseBindingK8sResourceEventsWithResponse(ctx, orgName,
		ocgen.ReleaseBindingNameParam(releaseBindingName),
		&ocgen.GetReleaseBindingK8sResourceEventsParams{
			Group:   &group,
			Version: "v1",
			Kind:    "Pod",
			Name:    podName,
		})
	if err != nil {
		return nil, fmt.Errorf("openchoreo: pod events for %s: %w", podName, err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	out := make([]RuntimeEvent, 0, len(resp.JSON200.Events))
	for _, e := range resp.JSON200.Events {
		ev := RuntimeEvent{Type: e.Type, Reason: e.Reason, Message: e.Message}
		if e.LastTimestamp != nil {
			ev.LastTimestamp = e.LastTimestamp.UTC()
		}
		out = append(out, ev)
	}
	return out, nil
}
