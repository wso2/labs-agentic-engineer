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

package io.ballerina.library.cache;

import com.google.gson.JsonElement;

import java.util.List;

/**
 * What a docs cache has to be able to do, and the implementation that does nothing.
 *
 * <p>The interface exists so nothing outside {@link DiskCache} reads the environment or touches a
 * filesystem, and so every test stays hermetic by getting {@link #NULL} and being unable to tell the
 * difference.
 *
 * <p>NOTHING HERE MAY THROW OR REPORT. Cache trouble is never the caller's problem: an unwritable
 * directory, a foreign owner, a full disk and a corrupt entry all have to come out as "no cached copy",
 * with no byte on stdout, no byte on stderr and no non-zero exit. The alternative — an unusable
 * {@code BAL_LIBRARY_CACHE_DIR} failing the run as {@code validation} — sends the agent into the skill's
 * argument-error advice in a loop it can never escape.
 *
 * <p>WHAT IS CACHED IS THE RAW PAYLOAD, not the IR and not the rendered string. The IR and the rendering are
 * our code, so an IR entry's key would need a build identity in it. The raw payload is not derived from our
 * code, so the coordinates are the whole key. Re-deriving costs about 200ms of parse and transform against
 * 5 to 7 seconds of download.
 *
 * <p>THE KEY HAS NO IDENTITY DIMENSION. That is correct only while the transport sends no headers and only
 * public Central data is reachable. If a Central token is ever threaded through the options, this cache must
 * be disabled or keyed by a token fingerprint: {@code $HOME} outlives the per-task workspace scrub, and a
 * 0600 mode buys nothing against the same user.
 *
 * @since 0.1.0
 */
public interface DocsCache {

    /** A package's immutable coordinates — the whole key of a docs entry. */
    record DocsKey(String org, String name, String version) { }

    /** A package without a version, which is what the versions list is keyed by. */
    record PackageKey(String org, String name) { }

    /** The one mutable answer Central gives, and when we last believed it. */
    record LatestEntry(String version, long atMs) { }

    /** The raw payload, or {@code null} for any reason whatsoever. */
    JsonElement readDocs(DocsKey key);

    void writeDocs(DocsKey key, JsonElement payload);

    /** Best-effort. Used to make a corrupt entry self-healing and by {@code --refresh}. */
    void removeDocs(DocsKey key);

    /** The cached versions answer, or {@code null}. */
    LatestEntry readLatest(PackageKey key);

    void writeLatest(PackageKey key, LatestEntry entry);

    /**
     * Every version of one package already on disk, newest first by version order. The offline fallback: a
     * warm payload plus one registry blip should not be a hard failure that burns the client's whole budget.
     */
    List<String> listVersions(PackageKey key);

    /**
     * One line for {@code --help}. The only place the cache is allowed to speak, and it is on stderr beside
     * usage text, outside both the document and the {@code Failure} contract — which is how an operator
     * proves the cache is alive in a runner image without parsing anything.
     */
    String describe();

    /** A cache that stores nothing and says so. */
    DocsCache NULL = new DocsCache() {

        @Override
        public JsonElement readDocs(DocsKey key) {
            return null;
        }

        @Override
        public void writeDocs(DocsKey key, JsonElement payload) {
        }

        @Override
        public void removeDocs(DocsKey key) {
        }

        @Override
        public LatestEntry readLatest(PackageKey key) {
            return null;
        }

        @Override
        public void writeLatest(PackageKey key, LatestEntry entry) {
        }

        @Override
        public List<String> listVersions(PackageKey key) {
            return List.of();
        }

        @Override
        public String describe() {
            return "disabled";
        }
    };
}
