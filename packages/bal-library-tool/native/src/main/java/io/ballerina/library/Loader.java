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

import io.ballerina.library.central.CentralClient;
import io.ballerina.library.central.DependenciesToml;
import io.ballerina.library.central.HttpOptions;
import io.ballerina.library.central.schema.CentralDocs;
import io.ballerina.library.model.FromCentral;
import io.ballerina.library.model.Pipeline;
import io.ballerina.library.views.Readmes;

/**
 * The whole capability in two steps: resolve which version to read, then read it once.
 *
 * <p>They are separate because the resolution rule is the part with a cost. An explicit version is free; a
 * {@code Dependencies.toml} is a file read; asking Central is a round trip. Callers that already know the
 * version should never pay for the ones that follow.
 *
 * <p>{@link #loadPackage} is deliberately the only load: all five verbs read the same payload and differ only
 * in which document they write from it, so a verb cannot be cheap because it skipped work another verb does.
 *
 * @since 0.1.0
 */
public final class Loader {

    private Loader() {
    }

    /**
     * How the version is pinned, on top of the transport options.
     *
     * <p>There is no {@code version} field and no way for a caller to supply one. That is the design: version
     * resolution is INTERNAL (§3.8), and the only input to it is which project the process is standing in.
     *
     * @param projectDir the Ballerina project the lookup is running inside, or {@code null} when it is not in one
     */
    public record LoadOptions(HttpOptions http, String projectDir) {

        public static LoadOptions of(HttpOptions http) {
            return new LoadOptions(http, null);
        }
    }

    /**
     * Which version of the package to read.
     *
     * <p>{@code Dependencies.toml} outranks Central's latest deliberately: once a build has resolved the
     * package, the locked version is the one the component will actually compile against, and reading a newer one
     * produces signatures that do not exist for this caller. It also bypasses the versions-list TTL entirely, so
     * caching changes nothing about this precedence.
     *
     * <p>The KNOWN LIMIT, accepted: outside a project, a lookup resolves Central's latest, which may differ from
     * what a build would pick. For a package not yet in the dependency graph, latest IS the correct answer — it is
     * what adding the import would resolve to.
     */
    public static Result<CentralClient.ResolvedVersion> resolveVersion(
            QualifiedName qualified, LoadOptions options) {
        if (options.projectDir() != null) {
            String locked = DependenciesToml.lockedVersion(options.projectDir(), qualified);
            if (locked != null) {
                return fixed(locked);
            }
        }
        return CentralClient.resolveLatestVersion(qualified, options.http());
    }

    /** A version a build already locked, taken as given rather than confirmed against the registry. */
    private static Result<CentralClient.ResolvedVersion> fixed(String input) {
        Result<Version> parsed = Version.parse(input);
        return parsed.isOk()
                ? Result.ok(new CentralClient.ResolvedVersion(parsed.value(), false, true))
                : parsed.cast();
    }

    /**
     * The one thing a caller has to be told about how this was loaded, or nothing.
     *
     * <p>Whether the bytes came off disk or off the wire is not the reader's business — the document is the
     * same either way, and the line saying so cost a row in every header while answering a question nobody
     * asked. What a caller cannot recover on their own is that the version was never confirmed: with the
     * registry unreachable the newest published version may be something else entirely, so every signature
     * below is a claim about a version this run took on faith.
     *
     * <p>Returned as {@code null} in the ordinary case, so the header carries nothing when there is nothing
     * to say. That also makes stdout run-order-independent again: the same command twice now prints the same
     * bytes, where the old provenance line printed {@code central} then {@code cache}.
     */
    public static String unverifiedWarning(boolean stale) {
        return stale
                ? "the registry was unreachable, so this version came off disk unchecked"
                : null;
    }

    public static Result<LoadedPackage> loadPackage(QualifiedName qualified, LoadOptions options) {
        Result<CentralClient.ResolvedVersion> resolved = resolveVersion(qualified, options);
        if (!resolved.isOk()) {
            return resolved.cast();
        }
        Version version = resolved.value().version();

        Result<CentralDocs> docs = CentralClient.fetchDocs(qualified, resolved.value(), options.http());
        if (!docs.isOk()) {
            return docs.cast();
        }

        Result<CentralDocs.Module> module = FromCentral.selectModule(docs.value(), qualified);
        if (!module.isOk()) {
            return module.cast();
        }

        return Result.ok(new LoadedPackage(
                qualified,
                version,
                Pipeline.build(module.value()),
                Readmes.collect(docs.value()),
                unverifiedWarning(resolved.value().stale())));
    }
}
