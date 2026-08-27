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

import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Package coordinates, obtainable only through the parser.
 *
 * <p>The error this prevents is the reader's single most common caller mistake: passing
 * {@code org/name:version} where {@code org/name} belongs. Both are strings, so a plain
 * {@code String} parameter cannot tell them apart and the mistake surfaces as a confusing "package
 * not found" from Central. A private constructor moves it to the type checker — a raw
 * {@code String} cannot reach a request builder at all.
 *
 * @since 0.1.0
 */
public final class QualifiedName {

    private static final Pattern PATTERN = Pattern.compile("^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)$");

    private final String org;
    private final String name;

    private QualifiedName(String org, String name) {
        this.org = org;
        this.name = name;
    }

    public String org() {
        return org;
    }

    public String name() {
        return name;
    }

    /**
     * The alias an {@code import} of this package puts in scope: {@code ballerinax/googleapis.gmail} →
     * {@code gmail}. Derived through {@link io.ballerina.library.model.ModuleRef} so there is one definition of
     * the rule, shared with the foreign references the renderer prefixes.
     */
    public String moduleAlias() {
        return new io.ballerina.library.model.ModuleRef(org, name).prefix();
    }

    /** {@code org/name}, the form Central's URLs and this CLI's argv both use. */
    public String qualified() {
        return org + "/" + name;
    }

    /**
     * {@code org/name:version} — the label every document and every failure identifies a lookup by.
     *
     * <p>One method because it was hand-built at four sites, and a label that differs between a
     * document header and the failure about that document is a label an agent cannot correlate.
     */
    public String versioned(Version version) {
        return qualified() + ":" + version.text();
    }

    public static Result<QualifiedName> parse(String input) {
        Matcher match = PATTERN.matcher(input.trim());
        if (!match.matches() || Version.isTraversal(match.group(1)) || Version.isTraversal(match.group(2))) {
            return Result.err(new Failure.Validation(
                    "Invalid package name '" + input + "'. Expected 'org/name' (no version suffix).",
                    "Drop any ':version' suffix and pass strictly 'org/name', e.g. 'ballerinax/github'."));
        }
        return Result.ok(new QualifiedName(match.group(1), match.group(2)));
    }

    @Override
    public boolean equals(Object other) {
        return other instanceof QualifiedName that && org.equals(that.org) && name.equals(that.name);
    }

    @Override
    public int hashCode() {
        return org.hashCode() * 31 + name.hashCode();
    }

    @Override
    public String toString() {
        return qualified();
    }
}
