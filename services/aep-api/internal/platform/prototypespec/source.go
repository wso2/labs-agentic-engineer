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

package prototypespec

// source.go — the static floor of the prototype.tsx check the Go gate can make
// without running it: the module fits the size cap, parses as TypeScript + JSX,
// and imports only react and @wso2/prototype-kit (the JSX runtime included).
// The agent's write gate makes the same checks and more (forbidden APIs and
// elements, navigation targets, and drawing every screen); this is the save
// gate's floor, so a file no one could run never reaches a tag. esbuild parses
// it, the same parser family the kit's transpiler targets, and is asked to
// resolve every import, which is where one outside the allow-list is refused.
// source-cases.json is the table both sides assert.

import (
	"fmt"
	"unicode/utf16"

	"github.com/evanw/esbuild/pkg/api"
)

// MaxSourceLength mirrors the kit's MAX_SOURCE_LENGTH, in UTF-16 code units
// like the TypeScript side counts.
const MaxSourceLength = 256 * 1024

var allowedImports = map[string]bool{"react": true, "react/jsx-runtime": true, "@wso2/prototype-kit": true}

const (
	importPlugin = "prototype-imports"
	importRule   = "a prototype imports only react and @wso2/prototype-kit, with static import declarations"
)

// CheckSource returns the static findings for a prototype.tsx body; none when
// it fits, parses and imports only what a prototype may.
func CheckSource(source string) []Finding {
	if n := len(utf16.Encode([]rune(source))); n > MaxSourceLength {
		return []Finding{{
			Code:     CodeSourceTooLarge,
			Location: "(file)",
			Message: fmt.Sprintf("prototype.tsx is %d characters; a prototype is at most %d. Split long mock data or remove unused screens.",
				n, MaxSourceLength),
		}}
	}
	result := api.Build(api.BuildOptions{
		Stdin:    &api.StdinOptions{Contents: source, Loader: api.LoaderTSX, Sourcefile: "prototype.tsx"},
		Bundle:   true,
		Write:    false,
		JSX:      api.JSXAutomatic,
		Platform: api.PlatformNeutral,
		Format:   api.FormatESModule,
		LogLevel: api.LogLevelSilent,
		Plugins:  []api.Plugin{importAllowList()},
		// An import runs whether or not a binding is read (the kit's
		// transpiler keeps unused imports too), so none is elided unchecked.
		TsconfigRaw: `{"compilerOptions":{"verbatimModuleSyntax":true}}`,
		Sourcemap:   api.SourceMapNone,
	})

	// A module that does not parse has one finding, like the kit's parser: the
	// first syntax error, since later ones are usually its echo.
	var findings []Finding
	for _, m := range result.Errors {
		f := Finding{Code: CodeSyntaxError, Location: "(file)", Message: m.Text}
		if m.PluginName == importPlugin {
			f.Code = CodeForbiddenImport
		}
		if m.Location != nil {
			f.Location = fmt.Sprintf("line %d", m.Location.Line)
		}
		if f.Code == CodeSyntaxError {
			return []Finding{f}
		}
		findings = append(findings, f)
	}
	return findings
}

// importAllowList marks the allowed imports external (nothing is read) and
// refuses every other one, and every dynamic import or require, with the rule.
func importAllowList() api.Plugin {
	return api.Plugin{
		Name: importPlugin,
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				switch args.Kind {
				case api.ResolveEntryPoint:
					return api.OnResolveResult{}, nil
				case api.ResolveJSDynamicImport:
					return api.OnResolveResult{}, fmt.Errorf("a dynamic import(): %s", importRule)
				case api.ResolveJSRequireCall, api.ResolveJSRequireResolve:
					return api.OnResolveResult{}, fmt.Errorf("require: %s", importRule)
				}
				if allowedImports[args.Path] {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				return api.OnResolveResult{}, fmt.Errorf("import of %q: %s", args.Path, importRule)
			})
		},
	}
}
