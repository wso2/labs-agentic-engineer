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

package io.ballerina.library.views;

import io.ballerina.library.LoadedPackage;
import io.ballerina.library.Texts;
import io.ballerina.library.model.Fn;
import io.ballerina.library.model.ModuleRef;
import io.ballerina.library.model.Param;
import io.ballerina.library.model.RecordField;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.model.TypeRef;
import io.ballerina.library.render.TypeDefs;
import io.ballerina.library.symbols.Declarations;

import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * {@code -r} — the transitive type closure, from a declaration or from a signature.
 *
 * <p>This was {@code --deps} on {@code type} and nothing else, which made the one flow it exists for cost two
 * calls: find the operation, then read its return type. It now starts from a CALLABLE as well, and the difference
 * is bigger than one saved round trip. The real closure for github's cache DELETE includes
 * {@code ActionsDeleteActionsCacheByKeyQueries} — the included-record parameter whose FIELDS are the call's named
 * arguments — which is the fact an agent needs to write the call at all and which no signature line spells out.
 *
 * <p><b>BREADTH-FIRST, AND BOUNDED.</b> The walk used to be depth-first and unbounded:
 * {@code type ballerina/http ClientConfiguration --deps} is 38 declarations, 505 lines, 24,183 bytes, handed back
 * whole. Breadth-first because a shallow field is likelier to be needed than a four-levels-deep one, so a budget
 * that truncates should truncate the far end. Every dropped name is NAMED rather than silently missing, which is
 * what keeps a truncated closure actionable: each one is a legal argument to {@code type}.
 *
 * <p><b>The roots are never dropped.</b> They are what was asked for, so they are charged before the budget is
 * consulted; a budget that could refuse the question is worse than a large answer.
 *
 * <p>Cross-package edges still stop at the package boundary and are NAMED rather than followed (ADR-0009).
 * {@code ballerina/http:ConnectionConfig} has a local closure of one and fifteen external edges, so crossing would
 * hide a cold fetch per edge inside an answer the caller expects to be warm.
 *
 * @since 0.1.0
 */
public final class Closure {

    /**
     * How many bytes of declarations a closure may print.
     *
     * <p>Sized against the measurement that motivated the bound rather than picked: {@code ClientConfiguration}'s
     * unbounded closure is 24,183 bytes, and 20,000 is the same figure the container listings use, so a caller
     * has one number to hold rather than three.
     */
    public static final int MAX_BYTES = 20_000;

    /** Every level. The depth argument exists for the one-level inlining a single-result view does. */
    public static final int UNBOUNDED = Integer.MAX_VALUE;

    /** Identifiers inside a rendered type expression. Builtins fall out by not being declarations. */
    private static final Pattern IDENTIFIER = Pattern.compile("[A-Za-z_][A-Za-z0-9_]*");

    private static final Pattern ALIAS_CHAR = Pattern.compile("[A-Za-z0-9_'.]");

    private Closure() {
    }

    /** One reference to a declaration in another package, as written. */
    public record ExternalRef(String prefix, String name) { }

    /**
     * A closure: the declarations to print, in walk order, and the ones the budget left out.
     *
     * @param names every declaration to render, roots first
     * @param omitted names reached but not printed, each a legal {@code type} argument
     */
    public record Result(List<String> names, List<String> omitted) {

        public boolean truncated() {
            return !omitted.isEmpty();
        }
    }

    /**
     * The closure of a set of declaration names.
     *
     * <p>The visited set is what makes it terminate on a cycle, and real packages have them — a record whose
     * field is an array of itself is ordinary.
     */
    public static Result of(List<String> roots, Declarations index, int budgetBytes, int maxDepth) {
        Set<String> visited = new LinkedHashSet<>();
        List<String> order = new ArrayList<>();
        Set<String> omitted = new LinkedHashSet<>();
        Deque<Step> queue = new ArrayDeque<>();

        for (String root : roots) {
            if (index.get(root) != null && visited.add(root)) {
                order.add(root);
                queue.add(new Step(root, 0));
            }
        }
        // The roots are charged but not checked: they are the question, and a budget that can refuse the
        // question is not a budget, it is a refusal.
        int spent = order.stream().mapToInt(name -> cost(name, index)).sum();

        while (!queue.isEmpty()) {
            Step step = queue.removeFirst();
            if (step.depth() >= maxDepth) {
                continue;
            }
            for (String referenced : localReferences(index.get(step.name()), index)) {
                if (visited.contains(referenced)) {
                    continue;
                }
                int size = cost(referenced, index);
                if (spent + size > budgetBytes) {
                    omitted.add(referenced);
                    continue;
                }
                visited.add(referenced);
                spent += size;
                order.add(referenced);
                queue.add(new Step(referenced, step.depth() + 1));
            }
        }
        // A name reached late may also have been printed early on another branch; the omission list is what the
        // caller has to ask for again, so anything printed is removed from it.
        omitted.removeAll(visited);
        return new Result(List.copyOf(order), List.copyOf(omitted));
    }

    /** One entry of the walk, carrying how far from a root it was reached. */
    private record Step(String name, int depth) { }

    private static int cost(String name, Declarations index) {
        TypeDef typeDef = index.get(name);
        return typeDef == null ? 0 : Texts.byteLength(TypeDefs.renderTypeDef(typeDef)) + 2;
    }

    /**
     * The same-package declarations a signature names — its parameters' types and its return's.
     *
     * <p>This is the entry point that makes {@code -r} work on a callable. Deliberately the same walk afterwards:
     * a signature's types are just another set of roots, so there is one closure implementation rather than one
     * per entry point that could disagree about cycles or budgets.
     */
    public static List<String> rootsOf(Fn fn, Declarations index) {
        List<String> roots = new ArrayList<>();
        for (Param param : fn.params()) {
            addLocalNames(param.type(), index, roots);
        }
        if (fn.returns().hasType()) {
            addLocalNames(fn.returns().type(), index, roots);
        }
        return List.copyOf(new LinkedHashSet<>(roots));
    }

    private static void addLocalNames(TypeRef type, Declarations index, List<String> into) {
        for (String token : partition(type.name()).local()) {
            if (index.get(token) != null && !into.contains(token)) {
                into.add(token);
            }
        }
    }

    /** Every same-package declaration one declaration mentions. */
    private static List<String> localReferences(TypeDef typeDef, Declarations index) {
        if (typeDef == null) {
            return List.of();
        }
        List<String> names = new ArrayList<>();
        for (TypeRef expression : expressionsOf(typeDef)) {
            for (String token : partition(expression.name()).local()) {
                if (index.get(token) != null && !names.contains(token)) {
                    names.add(token);
                }
            }
        }
        return List.copyOf(names);
    }

    // -----------------------------------------------------------------------
    // Reading a declaration's type expressions
    // -----------------------------------------------------------------------

    /**
     * Every type expression a declaration mentions.
     *
     * <p>Read off the rendered EXPRESSION rather than off Central's links, so the footer names exactly the
     * foreign spellings a reader can see above it. The links answer the other half — WHICH module an alias stands
     * for — and {@link #modulesByPrefix} reads them for that.
     */
    public static List<TypeRef> expressionsOf(TypeDef typeDef) {
        return switch (typeDef) {
            case TypeDef.Rec record -> record.fields().stream().map(RecordField::type).toList();
            case TypeDef.Alias alias -> List.of(alias.type());
            case TypeDef.Constant constant -> List.of(constant.varType());
            case TypeDef.Variable variable -> List.of(variable.varType());
            case TypeDef.ErrorDef error -> error.base().map(List::of).orElse(List.of());
            case TypeDef.Enumeration ignored -> List.of();
            // A class's dependencies are its members'. `-r` on `sql:Client` used to append nothing, because the
            // declaration it walked had no members to walk.
            case TypeDef.ObjectDef object -> objectExpressions(object);
        };
    }

    private static List<TypeRef> objectExpressions(TypeDef.ObjectDef object) {
        List<TypeRef> types = new ArrayList<>(object.fields().stream().map(RecordField::type).toList());
        for (Fn fn : object.methods()) {
            fn.params().forEach(param -> types.add(param.type()));
            if (fn.returns().hasType()) {
                types.add(fn.returns().type());
            }
        }
        return List.copyOf(types);
    }

    /** An expression's identifiers, split into same-package names and foreign ones. */
    public record Partition(List<String> local, List<ExternalRef> external) { }

    /**
     * Split an expression's identifiers into same-package names and foreign ones.
     *
     * <p>A token preceded by {@code :} is foreign and a token followed by {@code :} is the module alias itself,
     * which is how {@code http:Response} yields one external reference and no local one. Everything else is
     * looked up by the caller, so builtins ({@code string}, {@code map}, {@code anydata}) fall out for free by not
     * being declarations.
     */
    public static Partition partition(String expression) {
        List<String> local = new ArrayList<>();
        List<ExternalRef> external = new ArrayList<>();
        Matcher matcher = IDENTIFIER.matcher(expression);
        while (matcher.find()) {
            String token = matcher.group();
            int start = matcher.start();
            char before = start > 0 ? expression.charAt(start - 1) : '\0';
            char after = start + token.length() < expression.length()
                    ? expression.charAt(start + token.length())
                    : '\0';
            if (after == ':') {
                continue;
            }
            if (before == ':') {
                int aliasEnd = start - 1;
                int aliasStart = aliasEnd;
                while (aliasStart > 0
                        && ALIAS_CHAR.matcher(String.valueOf(expression.charAt(aliasStart - 1))).matches()) {
                    aliasStart--;
                }
                external.add(new ExternalRef(expression.substring(aliasStart, aliasEnd), token));
                continue;
            }
            local.add(token);
        }
        return new Partition(List.copyOf(local), List.copyOf(external));
    }

    // -----------------------------------------------------------------------
    // The cross-package footer
    // -----------------------------------------------------------------------

    /** Every foreign name the printed declarations mention, keyed so each is named once. */
    public static List<ExternalRef> externalRefs(List<String> names, Declarations index) {
        Map<String, ExternalRef> external = new LinkedHashMap<>();
        for (String name : names) {
            TypeDef typeDef = index.get(name);
            if (typeDef == null) {
                continue;
            }
            for (TypeRef expression : expressionsOf(typeDef)) {
                for (ExternalRef reference : partition(expression.name()).external()) {
                    external.put(reference.prefix() + ":" + reference.name(), reference);
                }
            }
        }
        return List.copyOf(external.values());
    }

    /**
     * Names from other packages, and the command that reads them.
     *
     * <p>ADR-0009: a foreign reference is three facts — the import path, the CLI coordinate, and whether an
     * import is needed at all. A {@code //} comment rather than prose because every document that carries this is
     * the code register, where a comment annotates real declarations instead of impersonating them.
     *
     * <p>The version is PRINTED and no longer passed as an argument. It is printed because these signatures were
     * generated against it and Central's latest may be a different one; it is not an argument because version
     * resolution is internal now, and a project's {@code Dependencies.toml} already pins the far side of the edge
     * — measured, {@code maintenance_api} imports 8 packages directly and locks 36.
     */
    public static String externalFooter(
            List<ExternalRef> references, LoadedPackage loaded, Set<String> printed) {
        if (references.isEmpty()) {
            return null;
        }
        Map<String, ModuleRef> modules = modulesByPrefix(loaded);
        Map<String, List<String>> byPrefix = new LinkedHashMap<>();
        for (ExternalRef reference : references) {
            ModuleRef module = modules.get(reference.prefix());
            // A pre-declared module is not an edge to cross: nothing to import, and the follow-up command for it
            // is a measured dead end (`type ballerina/lang.int Signed32` answers `// Unknown type: Signed32`).
            if (module != null && module.isPredeclared()) {
                continue;
            }
            byPrefix.computeIfAbsent(reference.prefix(), key -> new ArrayList<>()).add(reference.name());
        }
        if (byPrefix.isEmpty()) {
            return null;
        }

        List<String> prefixes = new ArrayList<>(byPrefix.keySet());
        prefixes.sort(Texts.LOCALE_ORDER);

        List<String> lines = new ArrayList<>();
        lines.add("// Declared in other modules, not included above:");
        lines.add("// Run one of these verbatim. The version beside each name is what these signatures were "
                + "generated against.");
        for (String prefix : prefixes) {
            List<String> names = new ArrayList<>(new LinkedHashSet<>(byPrefix.get(prefix)));
            names.sort(String::compareTo);
            lines.addAll(edgeLines(prefix, names, modules.get(prefix)));
            // SHEETS-03. `ProxyConfig` appeared in a list headed "not included above" in an output that declares
            // a `ProxyConfig` twelve lines earlier — two different records with the same name, the same arity and
            // different fields. Dropping the foreign one would lose the import the field line needs; saying which
            // is which is the fix, since the collision is the whole risk.
            List<String> collisions = names.stream().filter(printed::contains).toList();
            if (!collisions.isEmpty()) {
                boolean one = collisions.size() == 1;
                lines.add("//   note: " + String.join(", ", collisions)
                        + (one ? " above is this package's own declaration of that name"
                               : " above are this package's own declarations of those names")
                        + ", not " + prefix + "'s");
            }
        }
        return String.join("\n", lines);
    }

    /**
     * One edge: where the names live, and the command that reads them.
     *
     * <p>With no module resolved there is no command to offer, and the line says so instead of printing the alias
     * where a coordinate belongs — which is how {@code bal library type http Response} came to be printed, a
     * command that fails.
     */
    private static List<String> edgeLines(String prefix, List<String> names, ModuleRef module) {
        String joined = String.join(", ", names);
        if (module == null) {
            return List.of("//   " + joined + "  <-  " + prefix
                    + "  (module unknown: Central published no coordinate for this reference)");
        }
        String version = module.pinnedVersion().map(pinned -> " " + pinned).orElse("");
        return List.of(
                "//   " + joined + "  <-  " + module.coordinate() + version,
                "//   bal library type " + module.coordinate() + " " + String.join(" ", names) + " -r");
    }

    /**
     * Alias → module, recovered from the links the same payload published elsewhere.
     *
     * <p>The alias is all Central gives at a USE site, so the module it stands for comes from the declarations.
     * Keyed on the module's own prefix so the footer's {@code <-} mapping cannot disagree with the prefix the
     * signatures were printed with.
     */
    private static Map<String, ModuleRef> modulesByPrefix(LoadedPackage loaded) {
        Map<String, ModuleRef> modules = new LinkedHashMap<>();
        for (TypeDef typeDef : loaded.library().addressable()) {
            for (TypeRef expression : expressionsOf(typeDef)) {
                for (TypeRef.Link link : expression.links()) {
                    if (link instanceof TypeRef.Link.External ext) {
                        modules.put(ext.module().prefix(), ext.module());
                    }
                }
            }
        }
        return modules;
    }

    /**
     * The omission list, as the code register writes it.
     *
     * <p>Named rather than counted, because a name is a legal {@code type} argument and a count is not. The
     * marker is {@code //} — the tool's own voice — where {@code #} would be the package's doc comment.
     */
    public static String omissionComment(List<String> omitted) {
        if (omitted.isEmpty()) {
            return null;
        }
        return "// " + Texts.count(omitted.size()) + " more type" + (omitted.size() == 1 ? "" : "s")
                + " reached at the " + Texts.count(MAX_BYTES) + "-byte budget and not printed: "
                + String.join(", ", omitted) + "\n"
                + "// Ask for any of them directly — each is a name `bal library type` takes.";
    }
}
