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

package io.ballerina.library.constructs;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import io.ballerina.library.QualifiedName;
import io.ballerina.library.Result;
import io.ballerina.library.central.schema.CentralDocs;
import io.ballerina.library.central.schema.Schema;
import io.ballerina.library.model.FromCentral;
import io.ballerina.library.model.Library;
import io.ballerina.library.model.Pipeline;
import io.ballerina.library.render.Documents;

import java.util.ArrayList;
import java.util.List;

/**
 * A one-construct Central payload, and the real pipeline that turns it into Ballerina.
 *
 * <p>This is the whole point of the construct suite. The recorded corpus proves the pipeline against nine
 * real packages, which is what catches a regression in aggregate — but a 20,000-line snapshot diff does not
 * say WHICH language construct moved, and a construct no package in the corpus happens to use is not
 * covered at all even though every line of the code that would handle it reports as covered.
 *
 * <p>So a case here is a payload containing exactly one declaration, exercising exactly one syntax
 * dimension, run through {@link Pipeline} — {@link Schema} → {@link FromCentral} → patches → defaults —
 * and then {@link Documents}: the same path the {@code api} verb takes, with nothing stubbed. Through
 * {@code Pipeline} and not the stages by hand, because a suite that assembles its own pipeline stops
 * describing the product the moment one gains a stage. A fix that changes how closed records render
 * fails the closed-record case by name and leaves the other fifty-odd alone, which is the property that
 * makes the fidelity register's stages safe to land one at a time.
 *
 * <p>The package is {@code test/pkg} rather than a real name so that no per-package patch applies. Cases that exist to test a patch name the package the patch keys on.
 *
 * @since 0.1.0
 */
public final class Payload {

    /**
     * Every array {@link Schema} requires of a module. All present and empty by default, because a missing
     * one is schema drift rather than a package with none of them — so a case that omitted one would fail
     * as a parse error instead of testing its construct.
     */
    private static final List<String> CATEGORIES = List.of(
            "records",
            "stringTypes",
            "integerTypes",
            "decimalTypes",
            "booleanTypes",
            "simpleNameReferenceTypes",
            "arrayTypes",
            "unionTypes",
            "intersectionTypes",
            "anyDataTypes",
            "anyTypes",
            "tupleTypes",
            "functionTypes",
            "typeDescriptorTypes",
            "mapTypes",
            "streamTypes",
            "tableTypes",
            "xmlTypes",
            "errors",
            "constants",
            "enums",
            "classes",
            "objectTypes",
            "clients",
            "functions",
            "listeners",
            "serviceTypes",
            "annotations",
            "variables",
            "configurables");

    /**
     * The top-level sections {@link Documents} emits, in order. Matched exactly, because the generic
     * service form prints a {@code // --- Service (generic) ---} line INSIDE the service section and a
     * prefix match would treat it as a section of its own.
     */
    private static final List<String> SECTIONS = List.of(
            "// --- Types ---",
            "// --- Client ---",
            "// --- Functions ---",
            "// --- Listeners ---",
            "// --- Service ---",
            "// --- Annotations ---");

    private final JsonObject module = new JsonObject();

    private Payload(String org, String name) {
        module.addProperty("id", name);
        module.addProperty("orgName", org);
        for (String category : CATEGORIES) {
            module.add(category, new JsonArray());
        }
    }

    /** A module under a name no patch keys on. */
    public static Payload pkg() {
        return new Payload("test", "pkg");
    }

    /** A module under a real package name, for the cases that test a per-package patch. */
    public static Payload pkg(String org, String name) {
        return new Payload(org, name);
    }

    /** Put one declaration into one of Central's categories. */
    public Payload with(String category, Decl... declarations) {
        if (!module.has(category)) {
            throw new IllegalArgumentException(
                    "no such Central category: " + category + " — the schema reads " + CATEGORIES);
        }
        JsonArray array = module.getAsJsonArray(category);
        for (Decl declaration : declarations) {
            array.add(declaration.json());
        }
        return this;
    }

    private JsonObject raw() {
        JsonArray modules = new JsonArray();
        modules.add(module.deepCopy());
        JsonObject docsData = new JsonObject();
        docsData.add("modules", modules);
        JsonObject root = new JsonObject();
        root.add("docsData", docsData);
        return root;
    }

    /** The IR, or an assertion naming where the synthetic payload failed the schema. */
    public Library library() {
        String qualified = module.get("orgName").getAsString() + "/" + module.get("id").getAsString();
        Result<CentralDocs> parsed = Schema.parse(raw(), qualified);
        if (!parsed.isOk()) {
            throw new AssertionError(
                    "the synthetic payload does not parse: " + parsed.failure().describe());
        }
        Result<QualifiedName> name = QualifiedName.parse(qualified);
        if (!name.isOk()) {
            throw new AssertionError(qualified + " is not a package name");
        }
        Result<CentralDocs.Module> selected = FromCentral.selectModule(parsed.value(), name.value());
        if (!selected.isOk()) {
            throw new AssertionError("no module named " + qualified + " in the synthetic payload");
        }
        return Pipeline.build(selected.value());
    }

    /** The whole {@code api} document, header and all. */
    public String document() {
        return Documents.toSyntaxString(library());
    }

    /**
     * The document from its first section banner on, trailing newlines removed.
     *
     * <p>The header is four lines of package name that every case would otherwise repeat, and repeating it
     * fifty times would make the matrix about the header. The banner itself is KEPT: which section a
     * declaration lands in is part of what the reader promises, and two of the registered findings are
     * about a declaration landing in the wrong one or in none.
     */
    public String body() {
        String document = document();
        int start = document.indexOf("// --- ");
        if (start < 0) {
            // No section at all: every declaration was dropped. That is a finding in itself, so it is
            // reported as the empty body rather than as a test error.
            return "";
        }
        return document.substring(start).stripTrailing();
    }

    /**
     * One named section of the document, banner included.
     *
     * <p>Needed because a case naming a real package to reach a per-package patch gets that package's OTHER
     * patches too — asking for {@code ballerina/http} to test an error also injects http's service — and a
     * case about errors must not fail when the service injector changes. Scoping is how a case stays about
     * one construct.
     */
    public String section(String title) {
        String banner = "// --- " + title + " ---";
        if (!SECTIONS.contains(banner)) {
            throw new IllegalArgumentException("no such section: " + title + " — the document has " + SECTIONS);
        }
        List<String> collected = new ArrayList<>();
        boolean inside = false;
        for (String line : document().split("\n", -1)) {
            if (SECTIONS.contains(line)) {
                inside = line.equals(banner);
            }
            if (inside) {
                collected.add(line);
            }
        }
        return String.join("\n", collected).stripTrailing();
    }
}
