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

package io.ballerina.library.render;

import io.ballerina.library.model.ClientClass;
import io.ballerina.library.model.Library;
import io.ballerina.library.model.Param;
import io.ballerina.library.model.Service;

import java.util.ArrayList;
import java.util.List;
import java.util.stream.Collectors;

/**
 * {@link Library} → the whole-package Ballerina document the {@code api} verb prints.
 *
 * <p>The output is not a compilable module and is not meant to be: it is the package's whole public
 * surface written in the language the caller is about to write, so a signature can be read straight off it
 * instead of inferred from prose. Function bodies are {@code ;}, and a declaration the reader has no
 * Ballerina form for becomes a comment naming it.
 *
 * <p>Since the addressed verbs landed, this is the fallback rather than the default: {@code overview},
 * {@code ops} and {@code type} answer by name or by path, and {@code api} exists for the question none of
 * them answered, and so that a stale instruction telling an agent to grep one file is recoverable rather
 * than fatal.
 *
 * @since 0.1.0
 */
public final class Documents {

    private Documents() {
    }

    /**
     * Stamp a document's own length onto its first line — the tool's only defence against a filter.
     *
     * <p>Piping is not a habit this tool can argue a caller out of. It was measured at 19 of 19 calls before the
     * skill said "never pipe it" and 19 of 19 after, and the reflex turns out not to be about {@code bal library}
     * at all: in every session that showed it, a genuinely noisy command ({@code bal openapi}, {@code bal tool
     * pull}) had been piped moments earlier, and the {@code | head} came along with it — applied to
     * {@code bal library --help} before a single byte of a document had been seen. The one session with no such
     * command ahead of it piped nothing.
     *
     * <p>What that costs is silent. A {@code | head -150} over one github operation's type closure keeps 150 of
     * 535 lines and says nothing; one measured cut landed eight lines inside {@code public type Repository
     * record &#123;|}, so the model was handed a record whose last field had no closing brace. Bounded documents
     * do not help, because the bound is per document and the window is per call.
     *
     * <p>A length on line one converts that silence into arithmetic: 150 arrived, the document says 535. It is
     * the one line every window keeps, which is exactly why it goes there and not in a footer.
     *
     * <p>Applied at the single point every document passes through on its way to stdout, so a view written later
     * inherits it without knowing about it. A first line that is neither register's marker is left untouched.
     */
    public static String withLength(String document) {
        long lines = document.lines().count();
        int end = document.indexOf('\n');
        String first = end < 0 ? document : document.substring(0, end);
        String rest = end < 0 ? "" : document.substring(end);
        if (first.startsWith("<!-- bal library ") && first.endsWith(" -->")) {
            return first.substring(0, first.length() - " -->".length()) + " · " + lines + " lines -->" + rest;
        }
        // Every code-register document opens on a single-line identity comment — `// ballerinax/github:6.0.0`,
        // or `// Resolved: …` from `api`. Neither is a divider, so the count reads as part of the sentence.
        if (first.startsWith("// ")) {
            return first + " · " + lines + " lines" + rest;
        }
        return document;
    }

    /**
     * The opening comment of a code-register document: what was resolved, then what is wrong with it.
     *
     * <p>The counterpart of {@link Report#warning} for documents that ARE Ballerina, where a comment is the
     * only thing a file can carry. Both registers take the warning from the same loaded package, so a version
     * nobody verified says so whether the caller asked for Markdown or for source.
     *
     * @param warning {@code null} when there is nothing to warn about, and then no second line is written
     */
    public static String headerComment(String identity, String warning) {
        return warning == null ? "// " + identity : "// " + identity + "\n// Warning: " + warning;
    }

    /**
     * Section order is the output's contract with the caller: types, clients, functions, services,
     * annotations. Reordering it was proposed and rejected — it moves every declaration in all nine
     * snapshots and does not solve the motivating case, since {@code ballerinax/github}'s client section is
     * 2,715 lines on its own. The addressed verbs are the answer to "the client is at the bottom".
     */
    public static String toSyntaxString(Library library) {
        List<String> output = new ArrayList<>();
        output.add("// ============================================================");
        output.add("// Library: " + library.name());
        if (!library.description().isEmpty()) {
            output.add("// " + library.description().split("\n", -1)[0]);
        }
        output.add("// ============================================================");
        output.add("import " + library.name() + ";");

        section(output, "// --- Types ---", library.typeDefs().stream().map(TypeDefs::renderTypeDef).toList());
        section(output, "// --- Client ---",
                library.clients().stream().map(ClientClass::asObjectDef).map(TypeDefs::renderTypeDef).toList());
        section(output, "// --- Functions ---",
                library.functions().stream().map(Signatures::renderStandaloneFunction).toList());
        // Listeners immediately before the services written against them: a listener declaration and the
        // `service … on new Listener(…)` template are one story, and the template names the arguments whose
        // types only the declaration gives.
        section(output, "// --- Listeners ---",
                library.listeners().stream().map(TypeDefs::renderTypeDef).toList());
        section(output, "// --- Service ---", serviceSection(library.services()));
        section(output, "// --- Annotations ---",
                library.annotations().stream().map(Documents::renderAnnotation).toList());
        section(output, "// --- Configurables ---", configurableSection(library));

        output.add("");
        return String.join("\n", output);
    }

    /**
     * The {@code configurable} declarations, as COMMENTS, because they are not declarations a caller may write.
     *
     * <p>A {@code configurable} is module-private: {@code http:maxActiveConnections} from another module is
     * {@code attempt to refer to non-accessible symbol}, measured. So it cannot be printed as source in the code
     * register, where a declaration is something to copy — the same reason an unconfirmed service attachment is a
     * note here rather than a template.
     *
     * <p>It is here at all because {@code overview} stopped carrying it (ADR-0017) and this is the register that
     * still can. The entry document's section was addressed to a DEPLOYER rather than to someone writing a
     * {@code .bal} file, which is who that document is for; but the fact is real, {@code type} cannot reach it —
     * a configurable is not a declaration to resolve — and a fact reachable from no verb has been deleted rather
     * than moved. Expensive to reach is the intended cost. Unreachable was not.
     */
    private static List<String> configurableSection(Library library) {
        if (library.configurables().isEmpty()) {
            return List.of();
        }
        List<String> lines = new ArrayList<>();
        lines.add("// Set in the CALLER's Config.toml, under a [" + library.name().replace('/', '.')
                + "] table. These are\n// module-private, so they cannot be referenced from code — a default "
                + "above that a signature\n// also names is one you set here rather than pass.");
        for (Library.Configurable configurable : library.configurables()) {
            lines.add("// " + configurable.name() + " = " + configurable.defaultValue()
                    + "    # " + configurable.type().name()
                    + (configurable.description().isEmpty()
                            ? ""
                            : " — " + configurable.description().split("\n", -1)[0]));
        }
        return List.copyOf(lines);
    }

    private static void section(List<String> output, String title, List<String> rendered) {
        if (rendered.isEmpty()) {
            return;
        }
        output.add("");
        output.add(title);
        for (String item : rendered) {
            output.add("");
            output.add(item);
        }
    }

    // -----------------------------------------------------------------------
    // Services and annotations
    // -----------------------------------------------------------------------

    /**
     * The listener's constructor as ARGUMENTS, which is what a {@code new} call takes.
     *
     * <p>The parameter NAMES, because that is the only part of a declaration that belongs in a call. Emitting
     * the whole declaration produced six compiler errors on one line for kafka, the first of which was
     * {@code too many arguments in call to 'new()'} and the rest of which came from
     * {@code string|string[] bootstrapServers} being parsed as member access on {@code string}. A template a
     * caller is meant to copy has to parse.
     */
    private static String listenerArguments(Service.Listener listener) {
        return listener.initParams().stream()
                .map(Param::name)
                .collect(Collectors.joining(", "));
    }

    /** {@code kafka:Listener} → {@code kafka}, the alias the service type needs too. */
    private static String deriveListenerAlias(String listenerName) {
        int index = listenerName.indexOf(':');
        return index > 0 ? listenerName.substring(0, index) : null;
    }

    /**
     * How a service is written against this package: a template per service type a listener accepts, then one
     * note naming the types whose attachability cannot be established.
     *
     * <p>HTTP-14. Every service object type used to get {@code service X on new Listener(…)}, and 5 of the 10
     * the corpus produced do not compile: {@code ballerina/http}'s four interceptor types and
     * {@code ballerina/graphql}'s {@code Interceptor} are service objects a listener does not accept — an
     * interceptor reaches the runtime through {@code createInterceptors()}, not through an attachment.
     *
     * <p>The test is narrow — the listener's {@code attach} names this exact type — and it costs two forms that
     * WOULD have compiled: {@code http:ServiceContract} and {@code http:InterceptableService} both write
     * {@code *Service;} in the published source, and Central publishes no inclusion for an object type, so in
     * the payload they are indistinguishable from the four interceptors. Printing a declaration that does not
     * compile is the failure this document exists to prevent; withholding a template whose validity cannot be
     * established is a gap, and the note is what keeps it from being a silent one. Each of these types is
     * declared in full in the Types section either way, so no contract is lost.
     */
    private static List<String> serviceSection(List<Service> services) {
        List<String> rendered = new ArrayList<>();
        List<Service> unconfirmed = new ArrayList<>();
        for (Service service : services) {
            if (service.isAttachable()) {
                rendered.add(renderService(service));
            } else {
                unconfirmed.add(service);
            }
        }
        if (!unconfirmed.isEmpty()) {
            rendered.add(unconfirmedAttachments(unconfirmed));
        }
        return List.copyOf(rendered);
    }

    private static String unconfirmedAttachments(List<Service> unconfirmed) {
        String listener = unconfirmed.get(0).listener().name();
        String alias = deriveListenerAlias(listener);
        List<String> lines = new ArrayList<>();
        lines.add("// These service object types are declared above; this reader cannot confirm that "
                + listener);
        lines.add("// accepts them, so it writes no attachment template for them:");
        for (Service service : unconfirmed) {
            lines.add("//   " + (alias == null ? "" : alias + ":") + service.name());
        }
        lines.add("// " + listener + ".attach takes one specific type. A `distinct service object` type "
                + "reaches it only");
        lines.add("// by INCLUDING that type, and Central publishes no inclusion for an object type — so some "
                + "of these");
        lines.add("// do attach and some do not, and the payload cannot say which. An interceptor type, for "
                + "one, reaches");
        lines.add("// the runtime as a `createInterceptors()` return rather than as an attachment. The "
                + "package's own");
        lines.add("// guide is where the usage of each is written; `bal library overview` reproduces it.");
        return String.join("\n", lines);
    }

    private static String renderService(Service service) {
        List<String> lines = new ArrayList<>();
        if (service.isDeprecated()) {
            lines.add("@deprecated");
        }
        String alias = deriveListenerAlias(service.listener().name());
        String prefix = !service.name().isEmpty() && alias != null ? alias + ":" + service.name() + " " : "";
        lines.add("service " + prefix + "on new " + service.listener().name()
                + "(" + listenerArguments(service.listener()) + ") {");
        if (service.methods().isEmpty()) {
            // A SKELETON with a named hole, rather than a block that silently does not compile. Central
            // publishes no methods for `graphql:Service` or `kafka:Service`, and both listeners require one —
            // measured: `a GraphQL service must include at least one resource method with the accessor 'get'`
            // and `Service must have remote method onConsumerRecord`. `http:Service` is the case where an empty
            // body genuinely compiles, so the difference is real and only the package's guide states which
            // applies.
            lines.add("    // Central publishes no method contract for this service type. The listener may "
                    + "still require");
            lines.add("    // one — add the resource or remote methods the package's guide shows; "
                    + "`bal library overview`");
            lines.add("    // reproduces it.");
        }
        // The same renderer every other callable uses, so a service method keeps its parameter defaults, its
        // optionality, its doc comment and its import note. The hand-rolled copy this replaces kept none of
        // them.
        lines.addAll(TypeDefs.renderMembers(List.of(), service.methods()));
        lines.add("}");
        return String.join("\n", lines);
    }

    /**
     * An annotation declaration: its config record, its name, and every point it attaches to.
     *
     * <p>{@code public annotation HttpPayload Payload on parameter, return;} — the config record comes before the
     * name, which is the order the language reads it in and the reason the type can be dropped without the line
     * ceasing to parse. That is how {@code ResourceConfig} and {@code ServiceConfig} came to print as
     * {@code public annotation ResourceConfig on service_function;}: valid-looking, attached to a token the
     * compiler rejects, and giving no way to discover the field set of the record the attachment must carry.
     */
    private static String renderAnnotation(Library.AnnotationDef annotation) {
        List<String> lines = new ArrayList<>();
        if (!annotation.description().isEmpty()) {
            for (String line : annotation.description().split("\n", -1)) {
                lines.add("# " + line);
            }
        }
        List<Signatures.ExternalLink> links = annotation.type()
                .map(Signatures::collectExternalLinks)
                .orElse(List.of());
        String config = annotation.type()
                .map(type -> Signatures.applyPrefixToTypeName(type.name(), links) + " ")
                .filter(name -> !name.isBlank())
                .orElse("");
        lines.add("public annotation " + config + annotation.name() + " on " + annotation.attachmentPoints() + ";"
                + Signatures.buildSpecialAgentNote(links));
        return String.join("\n", lines);
    }
}
