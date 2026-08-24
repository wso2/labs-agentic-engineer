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

package io.ballerina.library;

import io.ballerina.library.model.Library;
import io.ballerina.library.views.Readmes;

import java.util.List;

/**
 * One package, read once: its coordinates, its API as the IR, and its guide.
 *
 * @param readmes every module of the payload that wrote a guide, in Central's order
 * @param warning why this version cannot be trusted, or {@code null} when it was confirmed against the
 *     registry — see {@link Loader#unverifiedWarning}
 * @since 0.1.0
 */
public record LoadedPackage(
        QualifiedName qualified,
        Version version,
        Library library,
        List<Readmes.ModuleReadme> readmes,
        String warning) {

    /** {@code org/name:version} — the label every document and every failure identifies this lookup by. */
    public String label() {
        return qualified.versioned(version);
    }

    /** The same package with a different IR, which is what a test that removes every client needs. */
    public LoadedPackage withLibrary(Library replacement) {
        return new LoadedPackage(qualified, version, replacement, readmes, warning);
    }
}
