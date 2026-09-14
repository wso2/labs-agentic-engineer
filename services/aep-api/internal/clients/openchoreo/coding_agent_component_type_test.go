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

import "testing"

func TestCodingAgentComponentType_Pins(t *testing.T) {
	ct := CodingAgentComponentType()
	if CodingAgentComponentTypeName != "coding-agent" {
		t.Fatalf("type name: %q", CodingAgentComponentTypeName)
	}
	meta, _ := ct["metadata"].(map[string]any)
	anns, _ := meta["annotations"].(map[string]string)
	if anns["aep.wso2.com/internal"] != "true" {
		t.Fatalf("missing internal annotation: %#v", anns)
	}
	spec, _ := ct["spec"].(map[string]any)
	if spec["workloadType"] != "job" {
		t.Fatalf("workloadType: %#v", spec["workloadType"])
	}
	// Assert schema defaults/max for backoffLimit, activeDeadlineSeconds,
	// ttlSecondsAfterFinished, and resources ceilings exist — exact path
	// depends on how the builder nests openAPIV3Schema; fail the test if
	// any pin is missing or wrong.
	schema := mustFindSchemaProps(t, spec)
	assertIntDefault(t, schema, "backoffLimit", 0)
	assertIntDefault(t, schema, "activeDeadlineSeconds", 3600)
	assertIntDefault(t, schema, "ttlSecondsAfterFinished", 86400)
	assertResourceCeilingsPresent(t, schema)
}

func mustFindSchemaProps(t *testing.T, spec map[string]any) map[string]any {
	t.Helper()
	// Navigate to parameters.openAPIV3Schema.properties — panic/fail if absent.
	params, _ := spec["parameters"].(map[string]any)
	oas, _ := params["openAPIV3Schema"].(map[string]any)
	props, _ := oas["properties"].(map[string]any)
	if props == nil {
		t.Fatal("missing openAPIV3Schema.properties")
	}
	return props
}

func assertIntDefault(t *testing.T, props map[string]any, key string, want int) {
	t.Helper()
	p, _ := props[key].(map[string]any)
	if p == nil {
		t.Fatalf("missing property %q", key)
	}
	def, ok := p["default"].(int)
	if !ok {
		// JSON numbers often decode as float64 in maps — accept both.
		if f, ok := p["default"].(float64); ok {
			def = int(f)
		} else {
			t.Fatalf("%s.default type %T", key, p["default"])
		}
	}
	if def != want {
		t.Fatalf("%s.default=%d want %d", key, def, want)
	}
}

func assertResourceCeilingsPresent(t *testing.T, props map[string]any) {
	t.Helper()
	for _, k := range []string{"cpuRequest", "cpuLimit", "memoryRequest", "memoryLimit"} {
		p, ok := props[k].(map[string]any)
		if !ok {
			t.Fatalf("missing resource schema key %q", k)
		}
		if _, ok := p["enum"]; !ok {
			t.Fatalf("%s must have an enum ceiling (requests and limits)", k)
		}
	}
}

// The runner may BURST but must not RESERVE what it burst to: a request is held
// for the pod's whole life and a limit is only a ceiling, so a request equal to
// the limit would hold cores idle through the model waits that are most of a
// run — and would out-weight every other pod on the node under contention,
// because cgroup CPU weight is proportional to requests.
func TestCodingAgentReservesFarLessCPUThanItMayBurstTo(t *testing.T) {
	ct := CodingAgentComponentType()
	spec, _ := ct["spec"].(map[string]any)
	props := mustFindSchemaProps(t, spec)

	req := props["cpuRequest"].(map[string]any)
	lim := props["cpuLimit"].(map[string]any)

	if req["default"] != "500m" {
		t.Errorf("cpuRequest default = %v, want 500m — the reservation must stay small", req["default"])
	}
	if lim["default"] != "3" {
		t.Errorf("cpuLimit default = %v, want 3", lim["default"])
	}
	// The ceiling is the schema's job, not the caller's: nothing passes these
	// parameters, so the enum is the only thing standing between a runner and
	// the whole node.
	enum, _ := lim["enum"].([]any)
	if len(enum) == 0 || enum[len(enum)-1] != "3" {
		t.Errorf("cpuLimit enum = %v, want a ceiling of 3", enum)
	}
	for _, v := range enum {
		if v == "4" || v == "6" || v == "8" {
			t.Errorf("cpuLimit enum offers %v — the k3d node has 6 vCPU shared with the whole stack", v)
		}
	}
}

// mustFindJobPodSpec digs the Job's pod template spec out of the CEL-templated
// resource list, so a test can assert on what the cluster will actually run.
func mustFindJobPodSpec(t *testing.T, spec map[string]any) map[string]any {
	t.Helper()
	resources, _ := spec["resources"].([]any)
	for _, r := range resources {
		res, _ := r.(map[string]any)
		if res["id"] != "job" {
			continue
		}
		tmpl, _ := res["template"].(map[string]any)
		jobSpec, _ := tmpl["spec"].(map[string]any)
		podTmpl, _ := jobSpec["template"].(map[string]any)
		podSpec, _ := podTmpl["spec"].(map[string]any)
		if podSpec == nil {
			t.Fatal("the job resource has no pod template spec")
		}
		return podSpec
	}
	t.Fatal(`no resource with id "job"`)
	return nil
}

// TestCodingAgentSizesDevShm closes an asymmetry that only ever bit in the
// cluster: Kubernetes gives a pod no /dev/shm of its own, so the container
// runtime supplies the 64Mi default, while BOTH other ways of running this exact
// image pass `--shm-size=1g` (runners/remote-worker/local/run-local.sh and the
// playground's docker run). The runner's mock-verification wave drives a
// headless Chromium, which does not degrade on a 64Mi /dev/shm — it aborts — so
// the image was developed and exercised under one shared-memory budget and
// dispatched under another.
//
// The medium is the point: /dev/shm has to be a tmpfs for Chromium's mmap'd
// shared buffers, and a default (disk-backed) emptyDir would provide the path
// without the semantics.
func TestCodingAgentSizesDevShm(t *testing.T) {
	ct := CodingAgentComponentType()
	spec, _ := ct["spec"].(map[string]any)
	podSpec := mustFindJobPodSpec(t, spec)

	containers, _ := podSpec["containers"].([]any)
	main, _ := containers[0].(map[string]any)
	mounts, _ := main["volumeMounts"].([]any)
	var shmVolume string
	for _, m := range mounts {
		mount, _ := m.(map[string]any)
		if mount["mountPath"] == "/dev/shm" {
			shmVolume, _ = mount["name"].(string)
		}
	}
	if shmVolume == "" {
		t.Fatalf("nothing is mounted at /dev/shm; the pod gets the runtime's 64Mi default\n%#v", mounts)
	}

	volumes, _ := podSpec["volumes"].([]any)
	var shm map[string]any
	for _, v := range volumes {
		vol, _ := v.(map[string]any)
		if vol["name"] == shmVolume {
			shm, _ = vol["emptyDir"].(map[string]any)
		}
	}
	if shm == nil {
		t.Fatalf("volume %q is not an emptyDir\n%#v", shmVolume, volumes)
	}
	if shm["medium"] != "Memory" {
		t.Errorf("/dev/shm medium = %v, want Memory — a disk-backed emptyDir is not shared memory", shm["medium"])
	}

	// The size is a SCHEMA pin like every other resource on this type, not a
	// literal buried in the template: a memory-backed emptyDir with no ceiling is
	// sized from the NODE's memory, and a pod that filled one would take the node
	// with it instead of being OOM-killed on its own.
	if shm["sizeLimit"] != "${parameters.shmSize}" {
		t.Errorf("/dev/shm sizeLimit = %v, want it bound to the schema parameter", shm["sizeLimit"])
	}
	props := mustFindSchemaProps(t, spec)
	shmSize, _ := props["shmSize"].(map[string]any)
	if shmSize == nil {
		t.Fatal("missing shmSize parameter")
	}
	if shmSize["default"] != "1Gi" {
		t.Errorf("shmSize default = %v, want 1Gi — the same budget run-local.sh and the playground pass", shmSize["default"])
	}
	if _, ok := shmSize["enum"]; !ok {
		t.Error("shmSize must have an enum ceiling, like every other resource pin on this type")
	}
}
