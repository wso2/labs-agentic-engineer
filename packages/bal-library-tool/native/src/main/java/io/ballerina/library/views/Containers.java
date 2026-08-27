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

import io.ballerina.library.Failure;
import io.ballerina.library.LoadedPackage;
import io.ballerina.library.Result;
import io.ballerina.library.Texts;
import io.ballerina.library.model.Fn;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.render.Documents;
import io.ballerina.library.render.Report;
import io.ballerina.library.render.Signatures;
import io.ballerina.library.render.TypeDefs;
import io.ballerina.library.symbols.Declarations;
import io.ballerina.library.symbols.Filter;
import io.ballerina.library.symbols.Names;
import io.ballerina.library.symbols.PathTree;
import io.ballerina.library.symbols.Surface;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

/**
 * {@code client} / {@code class} / {@code funcs} — one implementation, three scopes.
 *
 * <p>The three verbs differ ONLY in which slice of {@link Surface} they address. Everything below — how a
 * positional resolves, how a selector is parsed, how much is printed and what {@code ## Next} offers — is one code
 * path, because three copies of it would be three places for the same rule to rot separately.
 *
 * <p><b>THE PARSER CANNOT BE DECIDED PER VERB.</b> In Ballerina a client IS a class, so {@code ballerina/http:Client}
 * is a legal argument to both {@code client} and {@code class} and declares seven resource functions either way.
 * Confining HTTP-verb parsing to one verb would make {@code class ballerina/http Client delete repos/…} fail on a
 * container that has exactly that operation. So the CONTAINER is resolved first and the selector grammar is read
 * off what that container declares (§3.4).
 *
 * <p><b>A WRONG GUESS COSTS A LINE, NOT A ROUND TRIP.</b> Three rules do that work and all three were measured
 * defects: a name that is a MEMBER rather than a container still resolves and names its owner (T3); a symbol of
 * another kind still renders, with one line saying which verb is canonical (T6); and an exact one-of-many name
 * match prints the SIGNATURE rather than the name back (T15).
 *
 * <p><b>THE VIEWS QUOTE, THEY NEVER RE-SPELL.</b> Every declaration printed here comes from
 * {@link Signatures} or {@link TypeDefs} byte-for-byte. The tiers below choose HOW MANY declarations and how much
 * prose surrounds them; they never choose a different spelling. That is what {@code ViewsAgreeTest} pins, and the
 * reason it is a gate rather than a suite: a summariser permitted to invent a shorter form must pick one, and no
 * test can catch a spelling nothing else in the tool produces.
 *
 * @since 0.1.0
 */
public final class Containers {

    /**
     * How many bytes a whole container's signatures may take.
     *
     * <p>Measured either side: {@code googleapis.sheets}' 43 remote signatures are about 9KB and print in full,
     * while {@code twilio}'s 199 and {@code github}'s 903 fall to an index. Bytes rather than a count, because
     * one match is 200 bytes as a name and 900 as a resource signature — github's
     * {@code repos/&#123;owner&#125;/&#123;repo&#125;/actions/caches} is 723 bytes for three operations where
     * sheets averages 210 across forty-three.
     */
    public static final int MAX_LISTING_BYTES = 20_000;

    /**
     * How many bytes a FILTERED response may take.
     *
     * <p>Lower than a full listing, because a filter is a claim that the caller knows roughly what they want: the
     * real {@code -s "cache"} case on github is about 1.5KB, and a filtered answer that reaches 20KB means the
     * filter did not filter.
     */
    public static final int MAX_FILTERED_BYTES = 6_000;

    /** How many roster rows a listing prints before it prints a count instead. */
    public static final int MAX_ROSTER_ROWS = 20;

    /**
     * How many bytes of member names a MISS may offer before it prints a count instead.
     *
     * <p>Far below either listing budget, because a miss is not an answer: its job is to say what was asked for,
     * roughly what is there, and the command that lists it properly. See {@link #missComment} for the 42KB reply
     * to a github typo that set this.
     */
    private static final int MAX_MISS_NAME_BYTES = 1_500;

    /**
     * The shortest camelCase prefix that may name a cluster.
     *
     * <p>ADR-0019 measured what happens without it: splitting names into segments reads beautifully on
     * {@code twilio} ({@code list} 61, {@code fetch} 43, {@code create} 35) and produces {@code z} 20,
     * {@code s} 14, {@code h} 14, {@code l} 10 on {@code ballerinax/redis}, where the leading run of
     * {@code zAdd}/{@code hGet}/{@code lPush} is one letter. That is structure in the tokenizer rather than in the
     * domain, and nothing in the payload separates the two cases — but the PREFIX LENGTH does, exactly.
     */
    private static final int MIN_CLUSTER_PREFIX = 3;

    /** How many names a cluster needs before it is worth naming as one. */
    private static final int MIN_CLUSTER_SIZE = 3;

    /** Accessors a resource function may declare. Not a closed set — see {@link #isAccessor}. */
    private static final Pattern ACCESSOR = Pattern.compile("[a-z][a-zA-Z0-9]*");

    private Containers() {
    }

    /**
     * @param selectors everything after the package: a container name, a member, an accessor and a path
     * @param search the {@code -s} query, or {@code null}
     * @param resolve {@code -r}: answer in the code register, with the type closure
     * @param all {@code --all}: ignore the byte budget. Hidden from {@code --help}, offered only in {@code ## Next}
     */
    public record Options(List<String> selectors, String search, boolean resolve, boolean all) {

        public static Options bare() {
            return new Options(List.of(), null, false, false);
        }

        public Options(List<String> selectors) {
            this(selectors, null, false, false);
        }

        public boolean filtered() {
            return search != null && !search.isBlank();
        }

        /**
         * How many bytes this response may print.
         *
         * <p>NAMING A CONTAINER IS NOT A FILTER ON IT. {@code client ballerina/http Client} asked for that
         * container whole, so it gets the container budget — the narrower one applies once a SELECTOR or {@code -s}
         * has cut the container down, which is why this takes the selectors that survived container resolution
         * rather than reading the ones the caller typed.
         */
        int budget(List<String> remaining) {
            if (all) {
                return Integer.MAX_VALUE;
            }
            return filtered() || !remaining.isEmpty() ? MAX_FILTERED_BYTES : MAX_LISTING_BYTES;
        }
    }

    public static Result<String> render(LoadedPackage loaded, Surface.Scope scope, Options options) {
        return render(loaded, scope, options, null);
    }

    private static Result<String> render(
            LoadedPackage loaded, Surface.Scope scope, Options options, String note) {
        List<Surface.Container> containers = Surface.of(loaded.library(), scope);
        if (containers.isEmpty()) {
            return elsewhere(loaded, scope, options, "this package declares none");
        }

        List<String> selectors = options.selectors();
        if (selectors.isEmpty()) {
            if (containers.size() == 1) {
                return answer(loaded, scope, containers.get(0), List.of(), options, note);
            }
            return Result.ok(roster(loaded, scope, containers, options, note));
        }

        // 1. An exact container name always wins, so no casing heuristic is needed anywhere. `client sheets
        //    Client` is the container `Client` even in a package that also declares a member spelled that way.
        Optional<Surface.Container> named = Surface.byName(containers, selectors.get(0));
        if (named.isPresent()) {
            return answer(loaded, scope, named.get(), selectors.subList(1, selectors.size()), options, note);
        }

        // 2. One container in scope: whatever was typed is a selector for it.
        if (containers.size() == 1) {
            return answer(loaded, scope, containers.get(0), selectors, options, note);
        }

        // 3. An EXACT member name or a resolving path, across every container in scope. T3: this used to be a bare
        //    validation failure whose suggestion rebuilt the command WITHOUT the name that failed, so following it
        //    looped.
        Result<String> exact = byOwner(loaded, scope, containers, selectors, options, note, true);
        if (exact != null) {
            return exact;
        }

        // 4. Another verb's scope. BEFORE the substring pass, and that ordering is load-bearing: `Cookie` is a
        //    CLASS, and it is also a substring of `getCookieStore`, which two of http's ten clients declare. Run
        //    the fuzzy pass first and `client ballerina/http Cookie` answers with a roster of two clients that
        //    happen to contain those letters instead of routing to the class the caller plainly named.
        Result<String> other = elsewhereIfKnown(loaded, scope, options);
        if (other != null) {
            return other;
        }

        // 5. A substring of a member name, which is the widening a caller relies on when they half-remember one.
        Result<String> fuzzy = byOwner(loaded, scope, containers, selectors, options, note, false);
        if (fuzzy != null) {
            return fuzzy;
        }

        return elsewhere(loaded, scope, options, "no " + scope.verb() + " declares it");
    }

    /**
     * Whichever containers in this scope hold the selector: one is answered, several are a roster.
     *
     * <p>{@code exactly} is the two passes of §3.3 rather than two code paths — the resolution ORDER is the whole
     * subtlety here, and expressing it as one function called twice is what keeps the two passes from drifting into
     * different notions of a match.
     */
    private static Result<String> byOwner(
            LoadedPackage loaded, Surface.Scope scope, List<Surface.Container> containers,
            List<String> selectors, Options options, String note, boolean exactly) {
        Map<Surface.Container, List<Entry>> owners = new LinkedHashMap<>();
        for (Surface.Container container : containers) {
            List<Entry> hits = exactly
                    ? selectExactly(container, selectors)
                    : select(container, selectors);
            if (!hits.isEmpty()) {
                owners.put(container, hits);
            }
        }
        if (owners.isEmpty()) {
            return null;
        }
        if (owners.size() == 1) {
            Surface.Container owner = owners.keySet().iterator().next();
            return answer(loaded, scope, owner, selectors, options,
                    note != null ? note : ownerNote(loaded, scope, owner, selectors.get(0)));
        }
        return Result.ok(ownerRoster(loaded, scope, owners, selectors, options));
    }

    /**
     * Is this selector known to ANOTHER scope, or to the declaration roster?
     *
     * <p>Split out of {@link #elsewhere} so the resolution order can consult it without committing to a failure:
     * {@code null} means "not there either", which is what lets the substring pass run afterwards.
     */
    private static Result<String> elsewhereIfKnown(
            LoadedPackage loaded, Surface.Scope scope, Options options) {
        String token = options.selectors().get(0);
        for (Surface.Scope other : Surface.Scope.values()) {
            if (other == scope) {
                continue;
            }
            List<Surface.Container> containers = Surface.of(loaded.library(), other);
            if (Surface.byName(containers, token).isPresent()
                    || containers.stream().anyMatch(container ->
                            !selectExactly(container, options.selectors()).isEmpty())) {
                return render(loaded, other, options, kindNote(loaded, other, token));
            }
        }
        Declarations index = Declarations.index(loaded.library().addressable());
        if (Names.match(token, index.names()) instanceof Names.Match.Found found) {
            return TypeView.render(loaded, new TypeView.Options(
                    List.of(found.name()), null, options.resolve(),
                    "// Note: " + found.name() + " is a declaration, not a callable — showing it. Canonical: "
                            + "bal library type " + loaded.qualified().qualified() + " " + found.name()));
        }
        return null;
    }

    // -----------------------------------------------------------------------
    // Kind tolerance
    // -----------------------------------------------------------------------

    /**
     * A symbol this verb's scope does not hold, answered anyway.
     *
     * <p>This is what makes kind-specific verbs safe for an agent at all. Without it every kind guess risks a
     * wasted round trip; with it the split costs ONE PRINTED LINE. It closes T6, where
     * {@code ops <pkg> <constant>} failed with a client-ambiguity error for a name the package declares plainly.
     *
     * <p>The note is written in the register of the document it lands in — a facts row in a report, a {@code //}
     * comment in the code register — never a bare {@code note:} prefix, which fits neither.
     */
    private static Result<String> elsewhere(
            LoadedPackage loaded, Surface.Scope scope, Options options, String why) {
        if (options.selectors().isEmpty()) {
            return Result.ok(emptyScope(loaded, scope, why));
        }
        // The exact pass first, then a substring of a member in another scope — the same two-pass order the
        // in-scope resolution uses, for the same reason.
        Result<String> known = elsewhereIfKnown(loaded, scope, options);
        if (known != null) {
            return known;
        }
        String token = options.selectors().get(0);
        for (Surface.Scope other : Surface.Scope.values()) {
            if (other == scope) {
                continue;
            }
            if (Surface.of(loaded.library(), other).stream()
                    .anyMatch(container -> !select(container, options.selectors()).isEmpty())) {
                return render(loaded, other, options, kindNote(loaded, other, token));
            }
        }
        return Result.err(notFound(loaded, scope, token,
                Declarations.index(loaded.library().addressable())));
    }

    private static String kindNote(LoadedPackage loaded, Surface.Scope actual, String token) {
        return Texts.code(token) + " is addressed by " + Texts.code(actual.verb()) + " — showing it. "
                + "Canonical: " + Texts.code("bal library " + actual.verb() + " "
                + loaded.qualified().qualified() + " " + token);
    }

    private static String ownerNote(
            LoadedPackage loaded, Surface.Scope scope, Surface.Container owner, String token) {
        return Texts.code(token) + " is declared on " + Texts.code(owner.label()) + " — showing it. "
                + "Canonical: " + Texts.code("bal library " + scope.verb() + " "
                + loaded.qualified().qualified() + " " + owner.name() + " " + token);
    }

    /**
     * Nothing matched anywhere, with the names that came closest.
     *
     * <p>The suggestion must never rebuild the command WITHOUT the argument that failed — the shipped {@code ops}
     * path did exactly that, so an agent following it ran the same wrong shape again.
     */
    private static Failure notFound(
            LoadedPackage loaded, Surface.Scope scope, String token, Declarations index) {
        String pkg = loaded.qualified().qualified();
        List<String> candidates = new ArrayList<>();
        for (Surface.Scope other : Surface.Scope.values()) {
            Surface.of(loaded.library(), other).forEach(container -> {
                if (!container.isModule()) {
                    candidates.add(container.name());
                }
                candidates.addAll(container.memberNames());
            });
        }
        List<String> near = Names.nearMisses(token, List.copyOf(new LinkedHashSet<>(candidates)));
        if (near.isEmpty()) {
            near = Names.nearMisses(token, index.names());
        }
        String suggestion = near.isEmpty()
                ? "Nothing in " + loaded.label() + " is named anything like that. Search instead: "
                        + "`bal library " + scope.verb() + " " + pkg + " -s \"" + token + "\"`."
                : "The candidates are the closest names in the package. Re-run with one of them, or search: "
                        + "`bal library " + scope.verb() + " " + pkg + " -s \"" + token + "\"`.";
        return new Failure.SymbolNotFound(loaded.label(), List.of(token), near, suggestion);
    }

    /** A scope with nothing in it, saying where the callable surface actually is. */
    private static String emptyScope(LoadedPackage loaded, Surface.Scope scope, String why) {
        String pkg = loaded.qualified().qualified();
        Report report = new Report(scope.verb());
        report.heading(1, title(scope) + " — " + pkg);
        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact(title(scope), why));
        report.facts(facts);

        report.heading(2, "Next");
        List<String> next = new ArrayList<>();
        for (Surface.Scope other : Surface.Scope.values()) {
            if (other == scope) {
                continue;
            }
            List<Surface.Container> containers = Surface.of(loaded.library(), other);
            if (!containers.isEmpty()) {
                next.add(Texts.code("bal library " + other.verb() + " " + pkg) + " — "
                        + Texts.count(containers.size()) + " " + title(other).toLowerCase(java.util.Locale.ROOT)
                        + (other == Surface.Scope.MODULE
                                ? " (" + Texts.count(containers.get(0).functions().size()) + " functions)"
                                : ""));
            }
        }
        next.add(Texts.code("bal library overview " + pkg) + " — the whole map of this package");
        report.bullets(next);
        return report.toString();
    }

    // -----------------------------------------------------------------------
    // Rosters
    // -----------------------------------------------------------------------

    /**
     * Several containers and nothing to choose between them yet.
     *
     * <p>Every row ends in the command that OPENS it (ADR-0019): {@code overview}'s old unconditional pointer
     * returned "none in any client" on {@code ballerinax/aws.s3} and pointed back at {@code overview}, a two-call
     * loop carrying no information.
     */
    private static String roster(
            LoadedPackage loaded, Surface.Scope scope, List<Surface.Container> containers, Options options,
            String note) {
        String pkg = loaded.qualified().qualified();
        List<Surface.Container> selected = options.filtered()
                ? containers.stream()
                        .filter(container -> !filterEntries(container, options).surface().isEmpty())
                        .toList()
                : containers;

        Report report = new Report(scope.verb());
        report.heading(1, title(scope) + " — " + pkg);

        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        if (note != null) {
            facts.add(new Report.Fact("Note", note));
        }
        facts.add(new Report.Fact(title(scope), Texts.count(containers.size()) + ", listed below"));
        if (options.filtered()) {
            facts.add(new Report.Fact("Filter", Texts.code(options.search()) + " — "
                    + Texts.count(selected.size()) + " of " + Texts.count(containers.size())
                    + " declare a match"));
        }
        report.facts(facts);

        report.heading(2, "Next");
        List<String> next = new ArrayList<>();
        if (!selected.isEmpty()) {
            next.add("open one: " + Texts.code("bal library " + scope.verb() + " " + pkg + " "
                    + selected.get(0).name()));
        }
        next.add("search across all of them: " + Texts.code("bal library " + scope.verb() + " " + pkg
                + " -s \"<what it does>\""));
        report.bullets(next);

        if (selected.isEmpty()) {
            report.heading(2, "No match");
            report.paragraph("Nothing under " + Texts.code(scope.verb()) + " matches "
                    + Texts.code(options.search()) + ". Widen the query, or search the whole package with "
                    + Texts.code("bal library overview " + pkg + " -s \"" + options.search() + "\"") + ".");
            return report.toString();
        }

        report.heading(2, Texts.count(selected.size()) + " " + title(scope).toLowerCase(java.util.Locale.ROOT));
        List<Surface.Container> shown = selected.subList(0, Math.min(MAX_ROSTER_ROWS, selected.size()));
        report.bullets(shown.stream()
                .map(container -> rosterRow(pkg, scope, container))
                .toList());
        if (shown.size() < selected.size()) {
            report.paragraph(Texts.count(selected.size() - shown.size()) + " more, not listed. Narrow with "
                    + Texts.code("-s") + " rather than reading them all.");
        }
        return report.toString();
    }

    private static String rosterRow(String pkg, Surface.Scope scope, Surface.Container container) {
        return Texts.code(container.name()) + " — " + counts(container) + " · "
                + Texts.code("bal library " + scope.verb() + " " + pkg + " " + container.name());
    }

    /** What a container holds, split by call form because {@code ->} versus {@code .} is the fact a caller wants. */
    private static String counts(Surface.Container container) {
        List<String> parts = new ArrayList<>();
        int resources = container.operations().size();
        long remote = container.standalone().stream().filter(Fn.Remote.class::isInstance).count();
        long normal = container.standalone().stream().filter(Fn.Normal.class::isInstance).count();
        if (resources > 0) {
            parts.add(Texts.count(resources) + " resource");
        }
        if (remote > 0) {
            parts.add(Texts.count(remote) + " remote");
        }
        if (normal > 0) {
            parts.add(Texts.count(normal) + " normal");
        }
        return parts.isEmpty() ? "nothing callable" : String.join(", ", parts);
    }

    /**
     * One member name, declared on several containers.
     *
     * <p>The answer is the OWNERS with counts, each row ending in the command that opens it — never a bare
     * validation failure, which is what T3 recorded as the sweep's most-hit ergonomic bug.
     */
    private static String ownerRoster(
            LoadedPackage loaded, Surface.Scope scope, Map<Surface.Container, List<Entry>> owners,
            List<String> selectors, Options options) {
        String pkg = loaded.qualified().qualified();
        String token = selectors.get(0);
        Report report = new Report(scope.verb());
        report.heading(1, title(scope) + " — " + pkg + " " + Texts.code(token));

        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact("Requested", Texts.code(token)));
        facts.add(new Report.Fact("Declared on", Texts.count(owners.size()) + " of "
                + Texts.count(Surface.of(loaded.library(), scope).size()) + ", so this verb will not choose"));
        report.facts(facts);

        report.heading(2, "Next");
        Surface.Container first = owners.keySet().iterator().next();
        report.bullets(List.of("pick one: " + Texts.code("bal library " + scope.verb() + " " + pkg + " "
                + first.name() + " " + String.join(" ", selectors))));

        report.heading(2, Texts.count(owners.size()) + " owners");
        report.bullets(owners.entrySet().stream()
                .map(entry -> Texts.code(entry.getKey().name()) + " — " + Texts.count(entry.getValue().size())
                        + " match" + (entry.getValue().size() == 1 ? "" : "es") + " · "
                        + Texts.code("bal library " + scope.verb() + " " + pkg + " " + entry.getKey().name()
                                + " " + String.join(" ", selectors)))
                .toList());
        return report.toString();
    }

    // -----------------------------------------------------------------------
    // Selection inside one container
    // -----------------------------------------------------------------------

    /** One callable, with the path it is reached by when it has one. */
    public record Entry(Fn fn, List<String> path) {

        /** How it is addressed: {@code get repos/{owner}} for a resource, the bare name otherwise. */
        public String label() {
            return switch (fn) {
                case Fn.Resource resource -> resource.accessor() + " " + String.join("/", path);
                case Fn.Standalone named -> named.name();
                case Fn.Constructor ignored -> "init";
            };
        }

        /**
         * {@code ->} or {@code .} — DERIVED and always printed.
         *
         * <p>Both signature errors in the 2026-08-15 sweep came from this fact being absent: a database client
         * declares {@code remote function close()}, and {@code dbClient.close()} does not compile.
         */
        public String callForm() {
            return fn instanceof Fn.Remote || fn instanceof Fn.Resource ? "->" : ".";
        }
    }

    /**
     * What a selector selects inside one container.
     *
     * <p>THE GRAMMAR FOLLOWS THE CONTAINER. A container that declares resource functions reads
     * {@code get}/{@code post}/{@code delete} as an accessor and the token after it as a path; one that does not
     * reads the same token as a member name. So {@code class ballerina/http Cookie get} looks for a member called
     * {@code get}, finds none, and fails with {@code Cookie}'s actual members as candidates.
     */
    private static List<Entry> select(Surface.Container container, List<String> selectors) {
        return select(container, selectors, false);
    }

    /**
     * The same selection, restricted to matches a caller could not have meant by accident.
     *
     * <p>An exact member name or a path that resolves. The substring widening — which is what makes a
     * half-remembered name work — is deliberately excluded here, because it is what lets a container name in
     * another scope lose to letters inside an unrelated member (see the resolution order in {@link #render}).
     */
    private static List<Entry> selectExactly(Surface.Container container, List<String> selectors) {
        return select(container, selectors, true);
    }

    private static List<Entry> select(
            Surface.Container container, List<String> selectors, boolean exactly) {
        List<Entry> all = entriesOf(container);
        if (selectors.isEmpty()) {
            return all;
        }
        if (container.hasPaths()) {
            List<Entry> byPath = selectByPath(container, selectors);
            if (!byPath.isEmpty()) {
                return byPath;
            }
        }
        // A single token is a name filter; more than one on a name-shaped container is a caller who typed a path
        // at something that has none, and the empty result routes them to the recovery that says so.
        String token = selectors.get(0);
        if (selectors.size() > 1 && container.hasPaths()) {
            return List.of();
        }
        // `new` is the constructor, which Ballerina spells `init`. An INPUT alias only — the document still prints
        // `init`, because it prints what the package declares. Measured in two separate sweeps: an agent asked for
        // `Client new`, was told nothing matched on a container declaring 112 members, and found it as `init` on
        // the next call. The guess is correct in most languages an agent has read, and it cost a round trip twice.
        if (token.equalsIgnoreCase("new") && container.constructor().isPresent()) {
            List<Entry> constructor = all.stream().filter(entry -> entry.fn() instanceof Fn.Constructor).toList();
            if (!constructor.isEmpty()) {
                return constructor;
            }
        }
        // Against BOTH spellings of the token, because a label is built for prose while the token may have been
        // copied out of a fence. `post chat\.postMessage` and `post chat.postMessage` are the same operation.
        String readable = PathTree.readableSelector(token);
        List<Entry> exact = all.stream()
                .filter(entry -> entry.label().equals(token) || entry.label().equals(readable))
                .toList();
        if (exactly || !exact.isEmpty()) {
            // AN EXACT NAME NEVER LOSES TO A SUBSTRING. `Producer send` is one function; widened first it is two,
            // because `sendWithMetadata` also contains those letters — and two results is the difference between
            // the full declaration with its types and a pair of signature lines (T15).
            return exact;
        }
        return all.stream().filter(entry -> matchesName(entry, token)).toList();
    }

    /** A path request, split into the accessor that filters it and the path that anchors it. */
    private record PathRequest(String accessor, List<String> tokens) { }

    /** How a path selector reads against this container, or nothing when it is not one. */
    private static Optional<PathRequest> pathRequest(Surface.Container container, List<String> selectors) {
        if (!container.hasPaths()) {
            return Optional.empty();
        }
        if (selectors.size() >= 2 && isAccessor(container, selectors.get(0))) {
            return Optional.of(new PathRequest(selectors.get(0), PathTree.splitPath(selectors.get(1))));
        }
        if (selectors.size() == 1) {
            // An accessor and a path in ONE argument, which is what copying a whole line out of a fenced
            // signature produces: `get [PathParamType ...path]`. Split across two arguments every path spelling
            // already resolved; as one token only the display spelling did, because a one-token selector was
            // compared against an entry's LABEL and never reached the walk that understands the rest.
            //
            // Safe because a member name cannot contain whitespace, and gated on the container actually
            // declaring the accessor — otherwise `Producer send` would parse `Producer` as one.
            String[] words = selectors.get(0).trim().split("\\s+", 2);
            if (words.length == 2 && isAccessor(container, words[0])) {
                return Optional.of(new PathRequest(words[0], PathTree.splitPath(words[1])));
            }
            return Optional.of(new PathRequest(null, PathTree.splitPath(selectors.get(0))));
        }
        return Optional.empty();
    }

    /** Where a path selector landed, relocation and alternatives included. */
    private static Optional<PathTree.Located> located(
            Surface.Container container, List<String> selectors) {
        return pathRequest(container, selectors)
                .map(request -> PathTree.locate(PathTree.build(container.operations()), request.tokens()));
    }

    private static List<Entry> selectByPath(Surface.Container container, List<String> selectors) {
        Optional<PathRequest> request = pathRequest(container, selectors);
        Optional<PathTree.Located> located = located(container, selectors);
        if (request.isEmpty() || located.isEmpty()
                || !(located.get().resolution() instanceof PathTree.Resolution.Found found)) {
            return List.of();
        }
        List<Entry> entries = PathTree.operationsUnder(found.node()).stream()
                .map(operation -> new Entry(operation.fn(), operation.segments()))
                .toList();
        String accessor = request.get().accessor();
        return accessor == null
                ? entries
                : entries.stream()
                        .filter(entry -> entry.fn() instanceof Fn.Resource resource
                                && resource.accessor().equalsIgnoreCase(accessor))
                        .toList();
    }

    /**
     * The facts rows a path selector earns: where it landed, and what a wildcard did not take.
     *
     * <p>Both are DISCLOSURES rather than decorations. A relocated request answered at a deeper path has to say
     * so or the caller believes the tree is shaped differently than it is; a wildcard took one branch and the
     * answer is short by exactly the paths it names.
     */
    private static List<Report.Fact> pathFacts(Surface.Container container, List<String> selectors) {
        Optional<PathTree.Located> found = located(container, selectors);
        if (found.isEmpty()) {
            return List.of();
        }
        PathTree.Located located = found.get();
        List<Report.Fact> facts = new ArrayList<>();
        // The path AS RESOLVED, which is the only place placeholder normalisation is visible: `owner`,
        // `{owner}` and `[string owner]` all address the same segment, and a document that echoed only what was
        // typed would leave a caller unable to tell which spelling the tree actually holds.
        if (located.resolution() instanceof PathTree.Resolution.Found node
                && !node.path().isEmpty()) {
            facts.add(new Report.Fact("Path", Texts.code(String.join("/", node.path()))));
        }
        if (located.relocated() && !located.alternatives().isEmpty()) {
            facts.add(new Report.Fact("Located",
                    Texts.code(String.join("/", located.alternatives().get(0)))
                            + " — the only match for that segment under the requested prefix"));
        }
        if (located.resolution() instanceof PathTree.Resolution.Found node) {
            for (PathTree.Descent.Sibling other : node.alsoMatched()) {
                facts.add(new Report.Fact("Also matched",
                        Texts.code(String.join("/", other.path())) + " (" + Texts.count(other.total())
                                + "), not included here"));
            }
        }
        return List.copyOf(facts);
    }

    /**
     * Is this token an accessor rather than a member name?
     *
     * <p>Read off the container rather than from a closed set of HTTP methods: a resource accessor is an
     * identifier, and {@code subscribe} (websub, graphql) is as legal as {@code get}. So the test is whether some
     * resource function of THIS container declares it.
     */
    private static boolean isAccessor(Surface.Container container, String token) {
        if (!ACCESSOR.matcher(token).matches()) {
            return false;
        }
        return container.operations().stream()
                .anyMatch(operation -> operation.fn().accessor().equalsIgnoreCase(token));
    }

    /** A bare token matches anywhere in a name; {@code *} is a wildcard, as it is in a path. */
    private static boolean matchesName(Entry entry, String token) {
        // The fenced spelling widens too, or a caller who copied `chat\.postMessage` and then tried to widen it
        // would be told twice that nothing matches.
        return glob(token).matcher(entry.label()).matches()
                || glob(PathTree.readableSelector(token)).matcher(entry.label()).matches();
    }

    private static Pattern glob(String token) {
        String expanded = token.contains("*") ? token : "*" + token + "*";
        StringBuilder regex = new StringBuilder();
        String[] literals = expanded.split("\\*", -1);
        for (int index = 0; index < literals.length; index++) {
            if (index > 0) {
                regex.append(".*");
            }
            if (!literals[index].isEmpty()) {
                regex.append(Pattern.quote(literals[index]));
            }
        }
        return Pattern.compile(regex.toString(), Pattern.CASE_INSENSITIVE);
    }

    private static List<Entry> entriesOf(Surface.Container container) {
        List<Entry> entries = new ArrayList<>();
        container.constructor().ifPresent(constructor -> entries.add(new Entry(constructor, List.of())));
        for (PathTree.Operation operation : container.operations()) {
            entries.add(new Entry(operation.fn(), operation.segments()));
        }
        container.standalone().forEach(fn -> entries.add(new Entry(fn, List.of())));
        return List.copyOf(entries);
    }

    private static Filter.Split<Entry> filterEntries(Surface.Container container, Options options) {
        return Filter.apply(options.search(), entriesOf(container),
                entry -> Filter.surfaceOf(entry.fn()), entry -> Filter.docsOf(entry.fn()));
    }

    // -----------------------------------------------------------------------
    // The answer
    // -----------------------------------------------------------------------

    private static Result<String> answer(
            LoadedPackage loaded, Surface.Scope scope, Surface.Container container, List<String> selectors,
            Options options, String note) {
        List<Entry> selected = select(container, selectors);
        List<Entry> documented = List.of();

        if (options.filtered()) {
            Filter.Split<Entry> split = Filter.apply(options.search(), selected,
                    entry -> Filter.surfaceOf(entry.fn()), entry -> Filter.docsOf(entry.fn()));
            selected = split.surface();
            documented = split.documented();
        }

        // The register is a property of the DOCUMENT (§4.1), so it is decided BEFORE the answer's shape: a `-r`
        // response is nothing but declarations even when the selection came back empty, or a caller redirecting it
        // into a .bal file would get a Markdown table in the middle of their source.
        if (options.resolve()) {
            return Result.ok(codeAnswer(loaded, scope, container, selected, selectors, options, note));
        }
        if (selected.isEmpty() && documented.isEmpty()) {
            return Result.ok(nothingMatched(loaded, scope, container, selectors, options));
        }
        return Result.ok(reportAnswer(loaded, scope, container, selectors, selected, documented, options,
                note));
    }

    /**
     * A selector that matched nothing, answered with what IS there.
     *
     * <p>Exit 0 with the alternatives rather than a failure: an empty selection is a fact about the container,
     * and the caller's next move is in the document. The one thing it must not do is offer a command WITHOUT the
     * argument that failed.
     */
    private static String nothingMatched(
            LoadedPackage loaded, Surface.Scope scope, Surface.Container container, List<String> selectors,
            Options options) {
        String pkg = loaded.qualified().qualified();
        String asked = options.filtered() ? options.search() : String.join(" ", selectors);
        List<Entry> all = entriesOf(container);

        Report report = new Report(scope.verb());
        report.heading(1, title(scope) + " — " + pkg + containerSuffix(container));
        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact("Requested", Texts.code(asked)));
        facts.add(new Report.Fact("Matched", "nothing on " + Texts.code(container.label()) + ", which declares "
                + counts(container)));
        report.facts(facts);

        // Rule 2 of segment location: more than one occurrence is LISTED and the request stops there. Picking
        // one is exactly the failure anchoring exists to prevent.
        List<List<String>> alternatives = located(container, selectors)
                .map(PathTree.Located::alternatives)
                .orElse(List.of());

        report.heading(2, "Next");
        List<String> next = new ArrayList<>();
        if (alternatives.size() > 1) {
            next.add("pick one of the paths below: " + Texts.code("bal library " + scope.verb() + " " + pkg
                    + containerArgument(container) + " '" + String.join("/", alternatives.get(0)) + "'"));
        }
        next.add("widen it: " + Texts.code("bal library " + scope.verb() + " " + pkg
                + containerArgument(container) + " -s \"" + asked + "\"")
                + " — searches parameter and type names, and documentation");
        next.add("list what is there: " + Texts.code("bal library " + scope.verb() + " " + pkg
                + containerArgument(container)));
        report.bullets(next);

        if (alternatives.size() > 1) {
            report.heading(2, Texts.count(alternatives.size()) + " paths carry that segment");
            report.literal(alternatives.stream().map(path -> String.join("/", path)).toList());
            return report.toString();
        }

        if (container.hasPaths()) {
            PathTree tree = PathTree.build(container.operations());
            report.heading(2, "Top-level path segments");
            report.literal(Report.columns(tree.children().stream()
                    .map(child -> child.segment() + " " + Texts.count(child.total()))
                    .toList()));
        } else if (!all.isEmpty()) {
            report.heading(2, Texts.count(all.size()) + " declared here");
            report.literal(Report.columns(all.stream().map(Entry::label).toList()));
        }
        return report.toString();
    }

    // -----------------------------------------------------------------------
    // The report register
    // -----------------------------------------------------------------------

    private static String reportAnswer(
            LoadedPackage loaded, Surface.Scope scope, Surface.Container container, List<String> selectors,
            List<Entry> selected, List<Entry> documented, Options options, String note) {
        String pkg = loaded.qualified().qualified();
        Tier tier = tierFor(selected, container, selectors, options);

        Report report = new Report(scope.verb());
        report.heading(1, title(scope) + " — " + pkg + containerSuffix(container));

        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        if (note != null) {
            facts.add(new Report.Fact("Note", note));
        }
        if (!container.isModule()) {
            facts.add(new Report.Fact("Container", Texts.code(container.name()) + " — " + counts(container)));
        }
        if (!selectors.isEmpty()) {
            facts.add(new Report.Fact("Selector", Texts.code(String.join(" ", selectors)) + " — "
                    + Texts.count(selected.size()) + " of " + Texts.count(entriesOf(container).size())));
            facts.addAll(pathFacts(container, selectors));
        }
        if (options.filtered()) {
            facts.add(new Report.Fact("Filter", Texts.code(options.search()) + " — "
                    + Texts.count(selected.size()) + " by name or type, " + Texts.count(documented.size())
                    + " more by documentation only"));
        }
        facts.add(new Report.Fact("Showing", tier.describe(selected.size())));
        report.facts(facts);
        // ADR-0013: the rule is printed where it applies, and only when there IS a dropped branch. The rows say
        // WHICH paths; repeating the consequence on each would say it twice.
        if (facts.stream().anyMatch(fact -> "Also matched".equals(fact.label()))) {
            report.paragraph("A wildcard takes one branch. What follows is short by exactly the paths in the "
                    + "'Also matched' rows above — ask for those by name before concluding an operation does "
                    + "not exist.");
        }

        report.heading(2, "Next");
        report.bullets(nextBullets(loaded, scope, container, canonical(container, selectors), selected, tier,
                options));

        renderTier(report, tier, container, selected, loaded, options);

        if (!documented.isEmpty()) {
            // NAMED, never rendered — the 6-versus-33 measurement on github. One line, and every one of them is
            // promotable by name in a single follow-up call.
            report.heading(2, Texts.count(documented.size()) + " matched documentation only");
            report.literal(Report.columns(documented.stream().map(Entry::label).toList()));
        }
        return report.toString();
    }

    /** How much of the shared renderer to quote. Never a different spelling of a declaration. */
    private enum Tier {

        /** One result: everything the declaration documents, plus the types its signature names. */
        FULL,

        /** The description and the declaration, one per result. */
        SIGNATURE,

        /** One line each: the name or the path, no signature. */
        INDEX,

        /** Path roots, or camelCase prefix clusters, with counts. */
        GROUPED;

        String describe(int results) {
            return switch (this) {
                case FULL -> "the declaration in full, with the types it names";
                case SIGNATURE -> Texts.count(results) + " signature" + (results == 1 ? "" : "s");
                case INDEX -> Texts.count(results) + " by name, no signatures — over the byte budget";
                case GROUPED -> Texts.count(results) + " grouped, no names — well over the byte budget";
            };
        }
    }

    /**
     * The richest tier that fits.
     *
     * <p>Exactly one result is the case worth naming: T15 recorded that an exact one-of-many name match printed
     * the NAME back, forcing a second call for the signature the caller had already identified.
     */
    private static Tier tierFor(
            List<Entry> selected, Surface.Container container, List<String> remaining, Options options) {
        if (selected.size() == 1 && !options.all()) {
            return Tier.FULL;
        }
        int budget = options.budget(remaining);
        if (Report.ballerinaBytes(signatures(selected, container)) <= budget) {
            return Tier.SIGNATURE;
        }
        if (indexBytes(selected) <= budget) {
            return Tier.INDEX;
        }
        return Tier.GROUPED;
    }

    private static int indexBytes(List<Entry> selected) {
        return Report.ballerinaBytes(Report.columns(selected.stream().map(Entry::label).toList()));
    }

    /**
     * The signatures, from the one renderer.
     *
     * <p>A module function carries {@code public} and a member does not, so the renderer is chosen by scope
     * rather than by shape — the same split the API document draws.
     */
    private static List<String> signatures(List<Entry> entries, Surface.Container container) {
        return entries.stream()
                .map(entry -> container.isModule() && entry.fn() instanceof Fn.Standalone standalone
                        ? Signatures.renderStandaloneFunction(standalone)
                        : Signatures.renderSignature(entry.fn()))
                .toList();
    }

    private static void renderTier(
            Report report, Tier tier, Surface.Container container, List<Entry> selected, LoadedPackage loaded,
            Options options) {
        if (selected.isEmpty()) {
            return;
        }
        switch (tier) {
            case FULL -> renderFull(report, container, selected.get(0), loaded, options);
            case SIGNATURE -> renderByCallForm(report, container, selected);
            case INDEX -> {
                report.heading(2, Texts.count(selected.size()) + " by name");
                report.literal(Report.columns(selected.stream().map(Entry::label).toList()));
            }
            case GROUPED -> renderGrouped(report, container, selected);
        }
    }

    /**
     * One result, in full, with the declarations its signature names.
     *
     * <p>The inlining is a DEPTH-1 closure and it is what makes the common flow one call rather than two: the
     * caches DELETE on github names {@code ActionsDeleteActionsCacheByKeyQueries}, the included-record parameter
     * whose fields are the call's named arguments, and no signature line spells those out. Over budget it is
     * dropped and {@code -r} is offered instead, because a truncated record is worse than a named one.
     */
    private static void renderFull(
            Report report, Surface.Container container, Entry entry, LoadedPackage loaded, Options options) {
        report.heading(2, "The call — " + Texts.code(entry.callForm()));
        report.ballerina(List.of(container.isModule() && entry.fn() instanceof Fn.Standalone standalone
                ? Signatures.renderStandaloneFunction(standalone)
                : Signatures.renderMemberFunction(entry.fn(), "", Signatures.Detail.FULL)));

        Declarations index = Declarations.index(loaded.library().addressable());
        List<String> roots = Closure.rootsOf(entry.fn(), index);
        if (roots.isEmpty()) {
            return;
        }
        Closure.Result closure = options.resolve()
                ? Closure.of(roots, index, Closure.MAX_BYTES, Closure.UNBOUNDED)
                : Closure.of(roots, index, MAX_FILTERED_BYTES, 1);
        List<String> rendered = closure.names().stream()
                .map(name -> TypeDefs.renderTypeDef(index.get(name)))
                .toList();
        if (rendered.isEmpty()) {
            return;
        }
        report.heading(2, "The types it names — " + Texts.count(rendered.size()));
        report.ballerina(rendered);
        if (closure.truncated()) {
            report.paragraph(Texts.count(closure.omitted().size()) + " more reached the budget and are not "
                    + "printed: " + closure.omitted().stream().map(Texts::code)
                            .collect(Collectors.joining(", ")) + ".");
        }
    }

    /**
     * Signatures, split by call form, because {@code ->} versus {@code .} is the fact a caller came for.
     *
     * <p>A client declaring both halves is answered with BOTH (ADR-0019). {@code ballerina/http}'s {@code Client}
     * is 7 resource functions and 20 named ones, and the shipped view printed the 7 under a fact row reading
     * {@code (7 of 7)} — {@code execute}, {@code forward}, {@code submit}, the promise set and the circuit-breaker
     * controls were reachable from no verb at all.
     */
    private static void renderByCallForm(
            Report report, Surface.Container container, List<Entry> selected) {
        section(report, container, "Constructor", null,
                selected.stream().filter(entry -> entry.fn() instanceof Fn.Constructor).toList());
        section(report, container, "Resource functions", "-> and a path",
                selected.stream().filter(entry -> entry.fn() instanceof Fn.Resource).toList());
        section(report, container, "Remote functions", "->",
                selected.stream().filter(entry -> entry.fn() instanceof Fn.Remote).toList());
        section(report, container,
                container.isModule() ? "Module-level functions" : "Normal functions", ".",
                selected.stream().filter(entry -> entry.fn() instanceof Fn.Normal).toList());
    }

    private static void section(
            Report report, Surface.Container container, String label, String callForm, List<Entry> entries) {
        if (entries.isEmpty()) {
            return;
        }
        report.heading(2, label + " — " + Texts.count(entries.size())
                + (callForm == null ? "" : ", call with " + Texts.code(callForm)));
        report.ballerina(signatures(entries, container));
    }

    /**
     * Too many even to name: path roots, or camelCase clusters.
     *
     * <p>A cluster needs a prefix of at least {@value #MIN_CLUSTER_PREFIX} characters, which is the guard ADR-0019's
     * {@code redis} measurement demands: {@code z}, {@code s}, {@code h} and {@code l} are what a segment splitter
     * finds in {@code zAdd}/{@code hGet}/{@code lPush}, and they are structure in the tokenizer rather than in the
     * domain. Where clustering does not cover most of the names the flat list wins, capped and honest about it.
     */
    private static void renderGrouped(Report report, Surface.Container container, List<Entry> selected) {
        List<Entry> paths = selected.stream().filter(entry -> entry.fn() instanceof Fn.Resource).toList();
        if (!paths.isEmpty()) {
            PathTree tree = PathTree.build(container.operations());
            report.heading(2, Texts.count(tree.total()) + " operations across "
                    + Texts.count(tree.children().size()) + " top-level segments");
            report.literal(Report.columns(tree.children().stream()
                    .map(child -> child.segment() + " " + Texts.count(child.total()))
                    .toList()));
        }
        List<Entry> named = selected.stream().filter(entry -> !(entry.fn() instanceof Fn.Resource)).toList();
        if (named.isEmpty()) {
            return;
        }
        Map<String, List<Entry>> clusters = clustersOf(named);
        int clustered = clusters.values().stream().mapToInt(List::size).sum();
        if (clustered * 2 < named.size()) {
            report.heading(2, Texts.count(named.size()) + " by name");
            report.literal(Report.columns(named.stream().map(Entry::label).toList()));
            return;
        }
        report.heading(2, Texts.count(named.size()) + " named functions in "
                + Texts.count(clusters.size()) + " prefix groups");
        report.literal(Report.columns(clusters.entrySet().stream()
                .map(entry -> entry.getKey() + "* " + Texts.count(entry.getValue().size()))
                .toList()));
    }

    /** Names grouped by their leading lowercase run, where that run is long enough to mean something. */
    private static Map<String, List<Entry>> clustersOf(List<Entry> entries) {
        Map<String, List<Entry>> byPrefix = new LinkedHashMap<>();
        for (Entry entry : entries) {
            String prefix = leadingLowercase(entry.label());
            if (prefix.length() >= MIN_CLUSTER_PREFIX) {
                byPrefix.computeIfAbsent(prefix, key -> new ArrayList<>()).add(entry);
            }
        }
        Map<String, List<Entry>> clusters = new LinkedHashMap<>();
        byPrefix.forEach((prefix, members) -> {
            if (members.size() >= MIN_CLUSTER_SIZE) {
                clusters.put(prefix, members);
            }
        });
        return clusters;
    }

    private static String leadingLowercase(String name) {
        int end = 0;
        while (end < name.length() && Character.isLowerCase(name.charAt(end))) {
            end++;
        }
        return name.substring(0, end);
    }

    // -----------------------------------------------------------------------
    // The code register
    // -----------------------------------------------------------------------

    /**
     * {@code -r} — the answer as Ballerina, so it is pasteable whole.
     *
     * <p>The register is a property of the DOCUMENT rather than of the verb (§4.1, amending ADR-0008): a
     * {@code -r} response is nothing but declarations, whichever verb reached it, so it carries no fences, no
     * report marker and no Markdown tables.
     */
    /**
     * A miss in the code register: what was asked, what the container holds, and the way out — bounded.
     *
     * <p>Measured before this existed: {@code client ballerinax/github Client nosuchthingatall -r} answered with
     * 42,746 bytes, every one of 903 labels joined onto a single line, for a typo. The report register had always
     * answered the same miss in about 800 by printing the COUNT and pointing at the listing, so ADR-0020's budget
     * held everywhere except the path a caller reaches by making a mistake — the path least worth ten thousand
     * tokens, and the one where a wall of text is hardest to read past.
     *
     * <p>So names are offered only while they are cheap, and the count plus the recovery command carry the rest.
     * The command matters as much as the bound: ADR-0014 is about a recovery that named no next step.
     */
    private static String missComment(
            LoadedPackage loaded, Surface.Scope scope, Surface.Container container, List<String> selectors,
            Options options) {
        List<Entry> all = entriesOf(container);
        String asked = options.filtered() ? "\"" + options.search() + "\"" : String.join(" ", selectors);
        String opening = "// Nothing on " + container.label() + " matches " + asked;
        if (all.isEmpty()) {
            return opening + ". It declares nothing callable.";
        }
        String command = "bal library " + scope.verb() + " " + loaded.qualified().qualified()
                + " " + container.name();
        List<String> names = new ArrayList<>();
        int spent = 0;
        for (Entry entry : all) {
            spent += Texts.byteLength(entry.label()) + 2;
            if (spent > MAX_MISS_NAME_BYTES) {
                break;
            }
            names.add(entry.label());
        }
        // The count is of the WHOLE container either way, so "12 of 903" is legible as a sample rather than
        // readable as the container being smaller than it is.
        String held = names.size() == all.size()
                ? ". It declares: " + String.join(", ", names)
                : ". It declares " + all.size() + ", of which: " + String.join(", ", names) + ", …";
        return opening + held + "\n// Read them: " + command
                + "\n// Or search inside it: " + command + " -s \"" + Texts.plain(asked) + "\"";
    }

    private static String codeAnswer(
            LoadedPackage loaded, Surface.Scope scope, Surface.Container container, List<Entry> selected,
            List<String> selectors, Options options, String note) {
        Declarations index = Declarations.index(loaded.library().addressable());
        List<String> blocks = new ArrayList<>();
        blocks.add(Documents.headerComment(loaded.label(), loaded.warning()));
        if (note != null) {
            blocks.add("// Note: " + Texts.plain(note));
        }
        if (selected.isEmpty()) {
            blocks.add(missComment(loaded, scope, container, selectors, options));
            return String.join("\n\n", blocks) + "\n";
        }

        List<Entry> shown = selected;
        int budget = options.all() ? Integer.MAX_VALUE : MAX_LISTING_BYTES;
        List<String> rendered = signatures(shown, container);
        int spent = rendered.stream().mapToInt(Texts::byteLength).sum();
        if (spent > budget) {
            // The union across a whole container will usually exceed the budget, and that is expected rather
            // than an error: it truncates breadth-first and NAMES what it dropped, so an agent that over-asked
            // gets a bounded answer plus the labels it needs to ask again.
            int kept = 0;
            int running = 0;
            for (String signature : rendered) {
                running += Texts.byteLength(signature);
                if (running > budget) {
                    break;
                }
                kept++;
            }
            blocks.add("// " + Texts.count(selected.size() - kept) + " of " + Texts.count(selected.size())
                    + " signatures omitted at the " + Texts.count(budget) + "-byte budget. Narrow with a "
                    + "selector or -s, or ask for one by name.");
            shown = selected.subList(0, Math.max(1, kept));
            rendered = signatures(shown, container);
        }
        blocks.addAll(rendered);

        List<String> roots = new ArrayList<>();
        shown.forEach(entry -> Closure.rootsOf(entry.fn(), index).forEach(root -> {
            if (!roots.contains(root)) {
                roots.add(root);
            }
        }));
        Closure.Result closure = Closure.of(roots, index, Closure.MAX_BYTES, Closure.UNBOUNDED);
        closure.names().forEach(name -> blocks.add(TypeDefs.renderTypeDef(index.get(name))));

        String omission = Closure.omissionComment(closure.omitted());
        if (omission != null) {
            blocks.add(omission);
        }
        Set<String> printed = new LinkedHashSet<>(closure.names());
        List<Closure.ExternalRef> external = new ArrayList<>(Closure.externalRefs(closure.names(), index));
        String footer = Closure.externalFooter(external, loaded, printed);
        if (footer != null) {
            blocks.add(footer);
        }
        return String.join("\n\n", blocks) + "\n";
    }

    // -----------------------------------------------------------------------
    // Shared prose
    // -----------------------------------------------------------------------

    private static List<String> nextBullets(
            LoadedPackage loaded, Surface.Scope scope, Surface.Container container, List<String> selectors,
            List<Entry> selected, Tier tier, Options options) {
        String pkg = loaded.qualified().qualified();
        String verb = scope.verb();
        List<String> next = new ArrayList<>();

        if (tier == Tier.GROUPED || tier == Tier.INDEX) {
            next.add("narrow it: " + Texts.code("bal library " + verb + " " + pkg
                    + containerArgument(container) + " -s \"<what it does>\"")
                    + " — matches names, paths, parameter and type names, and documentation");
        }
        if (!selected.isEmpty() && tier != Tier.FULL) {
            Entry first = selected.get(0);
            next.add("one call and every type it needs: " + Texts.code("bal library " + verb + " " + pkg
                    + containerArgument(container) + " " + quoted(first.label()) + " -r"));
        }
        if (tier == Tier.FULL && !options.resolve()) {
            next.add("the whole type closure, not just one level: " + Texts.code("bal library " + verb + " "
                    + pkg + containerArgument(container) + " "
                    + quoted(selected.get(0).label()) + " -r"));
        }
        // `--all` is offered here and NOWHERE ELSE: it is an escape hatch, not part of the taught contract, so it
        // is hidden from `--help` and appears last with its cost stated.
        if ((tier == Tier.INDEX || tier == Tier.GROUPED) && !options.all() && !selected.isEmpty()) {
            int bytes = Report.ballerinaBytes(signatures(selected, container));
            next.add("last resort — every signature, unbudgeted: " + Texts.code("bal library " + verb + " "
                    + pkg + containerArgument(container) + selectorArgument(selectors) + " --all")
                    + " — " + Texts.count(selected.size()) + " signatures, " + Texts.count(bytes) + " bytes");
        }
        if (next.isEmpty()) {
            next.add("read a declaration whole: " + Texts.code("bal library type " + pkg + " <Name> [-r]"));
        }
        return next;
    }

    private static String quoted(String label) {
        return label.contains(" ") || label.contains("{") ? "'" + label + "'" : label;
    }

    /**
     * The selector as the tree spells it, where a path resolved.
     *
     * <p>A pointer offers the CANONICAL form, not the one the caller happened to type: {@code repos/owner/repo},
     * {@code repos/{owner}/{repo}} and {@code repos/[string owner]/[string repo]} all address one path, and a
     * command echoing the typed spelling teaches the reader whichever variant they arrived with.
     */
    private static List<String> canonical(Surface.Container container, List<String> selectors) {
        Optional<PathTree.Located> found = located(container, selectors);
        if (found.isEmpty()
                || !(found.get().resolution() instanceof PathTree.Resolution.Found node)
                || node.path().isEmpty()) {
            return selectors;
        }
        return List.of(String.join("/", node.path()));
    }

    private static String selectorArgument(List<String> selectors) {
        return selectors.isEmpty() ? "" : " " + selectors.stream().map(Containers::quoted)
                .collect(Collectors.joining(" "));
    }

    private static String containerArgument(Surface.Container container) {
        return container.isModule() ? "" : " " + container.name();
    }

    private static String containerSuffix(Surface.Container container) {
        return container.isModule() ? "" : " " + Texts.code(container.name());
    }

    private static String title(Surface.Scope scope) {
        return switch (scope) {
            case CLIENT -> "Clients";
            case CLASS -> "Classes";
            case MODULE -> "Module functions";
        };
    }
}
