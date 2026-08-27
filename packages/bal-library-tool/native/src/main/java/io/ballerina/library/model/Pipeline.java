/*
 *  Copyright (c) 2026, WSO2 LLC. (http://www.wso2.com)
 *
 *  WSO2 LLC. licenses this file to you under the Apache License,
 *  Version 2.0 (the "License"); you may not use this file except
 *  in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing,
 *  software distributed under the License is distributed on an
 *  "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 *  KIND, either express or implied.  See the License for the
 *  specific language governing permissions and limitations
 *  under the License.
 */

package io.ballerina.library.model;

import io.ballerina.library.central.schema.CentralDocs;

import java.util.Set;
import java.util.stream.Collectors;
import java.util.stream.Stream;

/**
 * Central's module → the finished {@link Library}, in one place.
 *
 * <p>It exists because there were two: the CLI built the IR in {@code Loader} and the recorded corpus built it
 * in its own fixture helper, so a stage that added a pass to one left the nine {@code .bal} snapshots
 * measuring a pipeline the tool does not run. That is a silent failure by construction — the oracle goes on
 * passing while it stops describing the product.
 *
 * <p>The order is not arbitrary. {@link Patches} runs after {@link FromCentral} because a correction needs
 * something to correct, and {@link Defaults} runs after {@code Patches} because a patch can inject the very
 * declaration that decides whether a printed default names something the document has.
 *
 * @since 0.1.0
 */
public final class Pipeline {

    private Pipeline() {
    }

    public static Library build(CentralDocs.Module module) {
        return Defaults.markUnwritable(
                Patches.applyPatches(FromCentral.fromCentral(module)), publishedButUnrendered(module));
    }

    /**
     * Names Central publishes that no section of the document prints.
     *
     * <p>Module-level {@code variables} and {@code configurables} are parsed and have no rendering yet, so a
     * default that names one — http has eight — is a name the caller CAN write and cannot find here. Feeding
     * them to {@link Defaults} keeps it from claiming the package does not export them. When they gain a
     * rendering this set collapses to nothing and the argument disappears with it.
     */
    private static Set<String> publishedButUnrendered(CentralDocs.Module module) {
        return Stream.concat(module.variables().stream(), module.configurables().stream())
                .map(CentralDocs.VariableDecl::name)
                .collect(Collectors.toSet());
    }
}
