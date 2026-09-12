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

// Drift guard, copied verbatim with the interceptor. `bal openapi` drops the
// contract's security block, so OPERATION_SCOPES is authored by hand; this
// test re-derives it from openapi.yaml and fails the build when they diverge.
import ballerina/lang.regexp;
import ballerina/test;
import ballerina/yaml;

// The only path-item keys that are operations. OpenAPI lets a path item also
// carry `parameters` (an array), `summary`, `description`, `servers`, `$ref`
// and `x-` extensions; reading one of those as an operation fails the test on a
// perfectly valid contract, so everything outside this list is skipped.
final readonly & string[] HTTP_METHODS = ["get", "put", "post", "delete", "options", "head", "patch", "trace"];

@test:Config
function testScopeTableMatchesContract() returns error? {
    json spec = check yaml:readFile("openapi.yaml");
    json[] documentSecurity = check (check spec.security).ensureType();
    map<json> paths = check (check spec.paths).ensureType();

    map<string> fromSpec = {};
    foreach [string, json] [path, item] in paths.entries() {
        map<json> pathItem = check item.ensureType();
        foreach [string, json] [method, op] in pathItem.entries() {
            if HTTP_METHODS.indexOf(method.toLowerAscii()) == () {
                continue;
            }
            map<json> operation = check op.ensureType();
            json[] security = operation.hasKey("security")
                ? check operation.get("security").ensureType()
                : documentSecurity;
            fromSpec[key(method, path)] = expected(security);
        }
    }

    map<string> fromTable = {};
    foreach OperationScope row in OPERATION_SCOPES {
        fromTable[row.method + " /" + string:'join("/", ...row.segments)] = row.scope ?: "#signed-in";
    }
    test:assertEquals(fromTable, fromSpec,
            "OPERATION_SCOPES does not match openapi.yaml's security blocks");
}

function key(string method, string path) returns string =>
    method.toUpperAscii() + " " + regexp:replaceAll(re `\{[^}]+\}`, path, "*");

function expected(json[] security) returns string {
    if security.length() == 0 {
        return PUBLIC;
    }
    json[] scopes = <json[]>(<map<json>>security[0]).get("oauth2");
    return scopes.length() == 0 ? "#signed-in" : scopes[0].toString();
}
