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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// EnsureComponentType converges the namespaced ComponentType onto the desired
// body: create it when it is absent, and bring an existing one up to date when
// what is stored has drifted from what the platform now ships.
//
// Get-or-create is not enough, and the reason is not cosmetic. The stored
// ComponentType IS the schema every dispatch is validated against, so an org
// seeded under an older platform build keeps validating today's dispatches
// against yesterday's bounds — raise a parameter past the old maximum and every
// existing org's next dispatch is rejected by its own stale copy while a
// freshly-created org works fine. Converging on conflict is what keeps the two
// halves of such a change in step.
//
// body is the raw CR shape from CodingAgentComponentType() (map[string]any).
// Posted via CreateComponentTypeWithBody so we avoid a hand-written converter
// into gen.ComponentType — JSON round-trip through the gen client is enough.
func (c *componentClient) EnsureComponentType(ctx context.Context, orgName string, body map[string]any) error {
	name := componentTypeNameFromBody(body)
	if name == "" {
		return fmt.Errorf("ensure componenttype: body metadata.name is required")
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ensure componenttype %q: marshal body: %w", name, err)
	}

	resp, err := c.oc.CreateComponentTypeWithBodyWithResponse(ctx, orgName, "application/json", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("ensure componenttype %q: %w", name, err)
	}

	switch {
	case resp.StatusCode() == http.StatusCreated || resp.StatusCode() == http.StatusOK:
		return nil
	case resp.StatusCode() == http.StatusConflict:
		return c.convergeComponentType(ctx, orgName, name, raw)
	default:
		return fmt.Errorf("ensure componenttype %q: %w", name, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		}))
	}
}

// convergeComponentType re-reads an existing ComponentType and PUTs the desired
// body when the stored spec no longer covers it. A full replace is correct
// here: the ComponentType is platform-owned end to end (nothing outside this
// package writes it), so the desired body is the whole truth rather than a
// subset to merge into somebody else's edits.
//
// GET and PUT both run inside retryStaleWrite, and the GET is inside it
// deliberately: OpenChoreo re-reads the CR per request, so a retry that replayed
// a body computed from an earlier read would keep losing the same race. See
// stale_write.go.
//
// The read is not just a precondition, it is the write's gate: seeding runs on
// the dispatch path, and an unconditional PUT per dispatch would race OC's
// controllers for no reason. Only drift writes.
func (c *componentClient) convergeComponentType(ctx context.Context, orgName, name string, raw []byte) error {
	wanted, err := desiredSpecAsJSON(raw)
	if err != nil {
		return fmt.Errorf("ensure componenttype %q: normalize desired spec: %w", name, err)
	}
	return retryStaleWrite(ctx, "componenttype/"+name, func(ctx context.Context) error {
		getResp, gerr := c.oc.GetComponentTypeWithResponse(ctx, orgName, ocgen.ComponentTypeNameParam(name))
		if gerr != nil {
			return fmt.Errorf("ensure componenttype %q: conflict but refetch failed: %w", name, gerr)
		}
		if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
			return fmt.Errorf("ensure componenttype %q: conflict but refetch failed: %w", name, handleErrorResponse(getResp.StatusCode(), ErrorResponses{
				JSON401: getResp.JSON401,
				JSON403: getResp.JSON403,
				JSON404: getResp.JSON404,
				JSON500: getResp.JSON500,
			}))
		}

		current, cerr := componentTypeSpecAsJSON(getResp.JSON200)
		if cerr != nil {
			return fmt.Errorf("ensure componenttype %q: read stored spec: %w", name, cerr)
		}
		if jsonCovers(current, wanted) {
			return nil
		}

		slog.InfoContext(ctx, "openchoreo: componenttype drifted from the shipped shape — updating",
			"componenttype", name, "namespace", orgName)
		putResp, perr := c.oc.UpdateComponentTypeWithBodyWithResponse(ctx, orgName, ocgen.ComponentTypeNameParam(name), "application/json", bytes.NewReader(raw))
		if perr != nil {
			return fmt.Errorf("ensure componenttype %q: update: %w", name, perr)
		}
		if putResp.StatusCode() == http.StatusOK || putResp.StatusCode() == http.StatusCreated {
			return nil
		}
		return fmt.Errorf("ensure componenttype %q: update: %w", name, handleErrorResponse(putResp.StatusCode(), ErrorResponses{
			JSON400: putResp.JSON400,
			JSON401: putResp.JSON401,
			JSON403: putResp.JSON403,
			JSON404: putResp.JSON404,
			JSON409: putResp.JSON409,
			JSON500: putResp.JSON500,
		}))
	})
}

// componentTypeSpecAsJSON renders a fetched ComponentType's spec back into the
// generic JSON shape the desired body is written in, so the two can be compared
// without a hand-written field-by-field diff that would go stale the moment the
// ComponentType grows a parameter.
func componentTypeSpecAsJSON(ct *ocgen.ComponentType) (map[string]any, error) {
	if ct == nil || ct.Spec == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(ct.Spec)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// desiredSpecAsJSON pulls spec out of the already-marshalled desired body. It
// round-trips through JSON rather than reading the map directly so both sides of
// the comparison carry the SAME Go types — a desired `3600` is an int in the
// literal and a float64 once it has been over the wire, and comparing those two
// directly reports drift on every call.
//
// Only spec is compared. It is the half that decides how a Job renders and what
// a dispatch is validated against; metadata is server-normalized, and rewriting
// the object because an annotation came back in a different shape would mean a
// PUT on every dispatch forever.
func desiredSpecAsJSON(raw []byte) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	spec, _ := body["spec"].(map[string]any)
	if spec == nil {
		return map[string]any{}, nil
	}
	return spec, nil
}

// jsonCovers reports whether got already carries everything want asks for.
//
// It is containment and not equality because the server legitimately returns
// more than we sent — defaulted fields, status-adjacent bookkeeping — and
// treating those additions as drift would rewrite the object on every dispatch.
// Lists are compared element-wise and in order: order is meaningful in the
// resource list (it is the render order), and a shorter or reordered stored list
// IS drift.
func jsonCovers(got, want any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, present := g[k]
			if !present || !jsonCovers(gv, wv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !jsonCovers(g[i], w[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(got, want)
	}
}

func componentTypeNameFromBody(body map[string]any) string {
	meta, _ := body["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	name, _ := meta["name"].(string)
	return name
}
