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

package io.ballerina.library.central;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import io.ballerina.library.QualifiedName;
import io.ballerina.library.Version;

/**
 * Does this payload actually describe the package and version we asked for?
 *
 * <p>A cache keyed by coordinates is only as good as its answer to that question. The key comes from our own
 * argv, so a mismatch means the file on disk is not what its path claims — a partially-written entry from an
 * older layout, a hand-copied file, a rename that went wrong. Any of those would otherwise serve one package's
 * signatures under another package's name, which is the single worst thing this reader could do.
 *
 * <p>It runs on the RAW JSON rather than the schema's output because the schema strips exactly the two fields
 * it needs: a module has no {@code version} in the IR and the root has no {@code apiDocsVersion}. Both are on
 * the wire — verified present in all nine fixtures. Adding them to the schema instead would make them required
 * reads and turn a cosmetic upstream change into a failed lookup, which is the trade the schema deliberately
 * does not make.
 *
 * <p>Module matching uses the REQUESTED name, for the same reason module selection does: a check that verifies
 * one module while the renderer reads another verifies nothing.
 *
 * @since 0.1.0
 */
public final class Coordinates {

    private Coordinates() {
    }

    public static boolean match(JsonElement raw, QualifiedName qualified, Version version) {
        if (raw == null || !raw.isJsonObject()) {
            return false;
        }
        JsonObject root = raw.getAsJsonObject();
        String apiDocsVersion = Json.string(root, "apiDocsVersion");
        if (apiDocsVersion == null || apiDocsVersion.isEmpty()) {
            return false;
        }

        JsonElement docsData = root.get("docsData");
        if (docsData == null || !docsData.isJsonObject()) {
            return false;
        }
        JsonElement modules = docsData.getAsJsonObject().get("modules");
        if (modules == null || !modules.isJsonArray()) {
            return false;
        }

        for (JsonElement entry : modules.getAsJsonArray()) {
            if (describes(entry, qualified, version)) {
                return true;
            }
        }
        return false;
    }

    private static boolean describes(JsonElement entry, QualifiedName qualified, Version version) {
        if (entry == null || !entry.isJsonObject()) {
            return false;
        }
        JsonObject module = entry.getAsJsonObject();
        String id = Json.string(module, "id");
        if (id == null || !qualified.org().equals(Json.string(module, "orgName"))) {
            return false;
        }
        boolean named = id.equals(qualified.name()) || id.startsWith(qualified.name() + ".");
        return named && version.text().equals(Json.string(module, "version"));
    }

    /** The first entry of a versions array, or {@code null} if it is not a non-empty array of strings. */
    static String newestVersion(JsonElement raw) {
        java.util.List<String> all = publishedVersions(raw);
        return all.isEmpty() ? null : all.get(0);
    }

    /**
     * Every version Central lists, newest first.
     *
     * <p>T10. {@code package-not-found} used to say "verify the version is published" while no verb could list
     * what was published — advice naming a step the caller had no way to take. Naming them in the failure is why
     * no {@code versions} verb is needed, and it is the only place they are ever printed.
     */
    static java.util.List<String> publishedVersions(JsonElement raw) {
        if (raw == null || !raw.isJsonArray()) {
            return java.util.List.of();
        }
        java.util.List<String> versions = new java.util.ArrayList<>();
        for (JsonElement entry : raw.getAsJsonArray()) {
            if (entry.isJsonPrimitive() && entry.getAsJsonPrimitive().isString()
                    && !entry.getAsString().isEmpty()) {
                versions.add(entry.getAsString());
            }
        }
        return java.util.List.copyOf(versions);
    }
}
