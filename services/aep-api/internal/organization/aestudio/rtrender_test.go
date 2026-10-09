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

package aestudio

// rtrender_test.go: a test-only evaluation of the RT's ${…} expressions with
// cel-go, the library OpenChoreo's template engine uses, so a template test
// checks what an expression renders to, not only how the YAML parses. It
// follows OC's rules for the parts this RT uses (internal/template/engine.go):
// a string that is one ${…} renders to the expression's native value, a
// string mixing text and ${…} interpolates, and a map value or list item that
// renders to oc_omit() is dropped. Variables are dyn: OC also type-checks
// against the schemas, which this does not (live proof covers that).

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"google.golang.org/protobuf/types/known/structpb"
)

// omitted stands for OC's oc_omit() sentinel in this harness.
const omitted = "\x00oc_omit\x00"

func celEnv(t *testing.T) *cel.Env {
	t.Helper()
	env, err := cel.NewEnv(
		ext.Strings(),
		cel.Variable("metadata", cel.DynType),
		cel.Variable("parameters", cel.DynType),
		cel.Variable("environmentConfigs", cel.DynType),
		cel.Function("oc_omit", cel.Overload("oc_omit", []*cel.Type{}, cel.DynType,
			cel.FunctionBinding(func(...ref.Val) ref.Val { return types.String(omitted) }))),
		// A stand-in for OC's oc_dns_label (which also truncates and hashes):
		// tests compare two renders that both go through it.
		cel.Function("oc_dns_label", cel.Overload("oc_dns_label_2", []*cel.Type{cel.StringType, cel.StringType}, cel.StringType,
			cel.BinaryBinding(func(a, b ref.Val) ref.Val { return types.String(fmt.Sprint(a.Value(), "-", b.Value())) }))),
	)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// renderRT renders a parsed template subtree (JSON) with vars.
func renderRT(t *testing.T, tmpl json.RawMessage, vars map[string]any) any {
	t.Helper()
	var tree any
	if err := json.Unmarshal(tmpl, &tree); err != nil {
		t.Fatal(err)
	}
	return renderNode(t, celEnv(t), tree, vars)
}

func renderNode(t *testing.T, env *cel.Env, node any, vars map[string]any) any {
	switch v := node.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range v {
			if r := renderNode(t, env, val, vars); r != omitted {
				out[k] = r
			}
		}
		return out
	case []any:
		out := []any{}
		for _, item := range v {
			if r := renderNode(t, env, item, vars); r != omitted {
				out = append(out, r)
			}
		}
		return out
	case string:
		return renderString(t, env, v, vars)
	default:
		return v
	}
}

func renderString(t *testing.T, env *cel.Env, s string, vars map[string]any) any {
	spans := celSpans(t, s)
	if len(spans) == 0 {
		return s
	}
	if len(spans) == 1 && strings.TrimSpace(s) == s[spans[0][0]:spans[0][1]] {
		return evalCEL(t, env, s[spans[0][0]+2:spans[0][1]-1], vars)
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(s[last:sp[0]])
		fmt.Fprint(&b, evalCEL(t, env, s[sp[0]+2:sp[1]-1], vars))
		last = sp[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// celSpans finds each ${…} as [start, end) by brace depth, skipping braces in
// quoted strings (OC's findCELSpans).
func celSpans(t *testing.T, s string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(s); {
		start := strings.Index(s[i:], "${")
		if start < 0 {
			break
		}
		start += i
		depth, pos := 1, start+2
		var quote byte
		for ; pos < len(s) && depth > 0; pos++ {
			c := s[pos]
			switch {
			case quote != 0 && c == '\\':
				pos++
			case quote != 0 && c == quote:
				quote = 0
			case quote != 0:
			case c == '\'' || c == '"':
				quote = c
			case c == '{':
				depth++
			case c == '}':
				depth--
			}
		}
		if depth != 0 {
			t.Fatalf("unterminated expression in %q", s)
		}
		spans = append(spans, [2]int{start, pos})
		i = pos
	}
	return spans
}

func evalCEL(t *testing.T, env *cel.Env, expr string, vars map[string]any) any {
	t.Helper()
	ast, iss := env.Compile(expr)
	if iss.Err() != nil {
		t.Fatalf("compile %q: %v", expr, iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("program %q: %v", expr, err)
	}
	val, _, err := prg.Eval(vars)
	if err != nil {
		t.Fatalf("eval %q: %v", expr, err)
	}
	native, err := val.ConvertToNative(reflect.TypeOf(&structpb.Value{}))
	if err != nil {
		t.Fatalf("convert %q: %v", expr, err)
	}
	return dropOmitted(native.(*structpb.Value).AsInterface())
}

// dropOmitted removes oc_omit() inside a rendered map or list value.
func dropOmitted(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if val == omitted {
				delete(x, k)
			} else {
				x[k] = dropOmitted(val)
			}
		}
	case []any:
		out := []any{}
		for _, item := range x {
			if item != omitted {
				out = append(out, dropOmitted(item))
			}
		}
		return out
	}
	return v
}

// rtVars are the render inputs of a typical local install, the schema
// defaults applied, with the relay channel relayURL ("" = no relay).
func rtVars(relayURL string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"name": "r-ae-studio-dev-1234", "namespace": "dp-default-ae-system-dev", "resourceNamespace": "default",
			"labels": map[string]any{"openchoreo.dev/resource": "ae-studio"},
		},
		"parameters": map[string]any{
			"images":          map[string]any{"designAgent": "agent:1", "collab": "collab:1", "studioTools": "tools:1"},
			"org":             map[string]any{"id": "ou-1", "handle": "default"},
			"modelConnection": "", "githubOwner": "acme", "webhookRelayUrl": relayURL,
			"secrets": map[string]any{
				"designAgent": map[string]any{"rev": "", "data": []any{}},
				"studioTools": map[string]any{"rev": "r1", "data": []any{map[string]any{"env": "GITHUB_PAT", "key": "k", "property": "token"}}},
			},
		},
		"environmentConfigs": map[string]any{
			"gatewayHost": "openchoreoapis.localhost", "publicScheme": "http", "publicPortSuffix": ":19080",
			"listenerName": "http", "consoleOrigins": []any{"http://console.ae.localhost:8080"},
			"idp": map[string]any{"issuer": "http://idp", "jwksUrl": "http://idp/jwks", "tokenUrl": "http://idp/token",
				"userAudiences": []any{"APP_FACTORY_CONSOLE"}},
			"aepApiBaseUrl": "http://aep-api", "aeOnlyClientId": "ae-only", "runtimeClassName": "", "cilium": false,
			"storage":      map[string]any{"sizeLimit": "3Gi", "ephemeralRequest": "1Gi", "budgetBytes": "2147483648"},
			"resources":    map[string]any{"cpuRequest": map[string]any{"designAgent": "100m", "collab": "50m", "studioTools": "100m"}},
			"pullSecret":   map[string]any{"remoteKey": "", "property": ""},
			"extraEgress":  []any{},
			"webhookRelay": map[string]any{"image": "ghcr.io/chmouel/gosmee@sha256:abc"},
		},
	}
}
