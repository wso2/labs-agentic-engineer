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
import io.ballerina.library.model.ClientClass;
import io.ballerina.library.model.Fn;
import io.ballerina.library.model.Library;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.symbols.Names;
import io.ballerina.library.symbols.PathTree;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

/**
 * The worked code inside a package's readme, selected, ordered and checked against this version.
 *
 * <p>WHY THIS EXISTS. {@code overview} used to append the whole readme so that an agent always had a worked
 * example and the package's own practices. Measured over the eleven packages of the 2026-08-15 sweep, it almost
 * never arrived: {@code head -100} — the window agents actually write — delivered a worked snippet in 0 of 11.
 * The guide was 44% of the corpus and its CODE was 12% of it, so four fifths of what pushed the snippets past the
 * cut was prose the reader could not act on. This keeps the fifth that compiles and sends the rest to
 * {@code bal library guide}.
 *
 * <p>WHAT IT KEEPS. A block is kept when it DEMONSTRATES THE API — it constructs a client, calls one, or attaches
 * a service to a listener. Selection is by content and never by position, which is not a nicety: slack's four
 * blocks are an import, a construction, the {@code chat.postMessage} call and a {@code bal run}, and "the first
 * two" would keep the import and drop the call form whose absence caused both signature errors in that sweep.
 *
 * <p>WHAT IT NEVER DOES IS TRUNCATE ONE. ADR-0008 has the views quoting the register rather than paraphrasing
 * it, and half a snippet is a paraphrase with the compiler's half missing — kafka's listener example declares its
 * {@code consumerConfiguration} nine lines above the {@code service} that uses it. The budget selects whole
 * blocks; a block that alone exceeds it is dropped in favour of the pointer.
 *
 * <p>Two properties of quoted code follow from that and are accepted rather than fixed: a block may contain a
 * literal {@code ...} placeholder (kafka's listener example does), and a block may use a variable another block
 * declared. Both are the package's own text, which the sentence {@code Overview} prints above the section says.
 *
 * <p>THE CHECK IS ABOUT NAMES AND NOT TYPES, which is a named gap rather than an oversight. Measured on the
 * eleven-package corpus, one selected block does not compile for a reason no name resolution can see:
 * {@code ballerinax/redis}'s {@code string value = check redis->get("key")}, where {@code get} is declared and
 * returns {@code string|Error?}, so {@code check} yields {@code string?}. Closing it means union and nil
 * arithmetic against a declared type, on code that legitimately references variables another block declared —
 * a different kind of check, and ADR-0017 records it as its own decision rather than smuggling it in here.
 *
 * @since 0.1.0
 */
public final class Snippets {

    /**
     * How many lines of quoted code {@code overview} carries.
     *
     * <p>Sized against the corpus rather than picked: 40 lines is what fits all three roles for the widest
     * package measured — kafka's producer construction, one {@code ->send} and the listener service come to 34
     * — while leaving {@code overview} short enough that {@code ## Next} is inside a {@code head -100} for
     * every package in that corpus. Blocks are whole, so a real section lands under the cap rather than on it.
     */
    public static final int MAX_USAGE_LINES = 40;

    /** Fence languages this reads. Everything else is a shell transcript or a config file. */
    private static final Set<String> BALLERINA_FENCES = Set.of("ballerina", "bal");

    /**
     * A construction, in either spelling Ballerina allows.
     *
     * <p>The optional name between {@code new} and the bracket is the explicit-class form, and leaving it out
     * cost a real snippet: {@code googleapis.gmail}'s quickstart writes {@code check new gmail:Client(…)}, so a
     * pattern anchored on {@code new (} matched nothing in it and the block was dropped — taking with it the
     * three {@code configurable … = ?} lines whose absence cost one sweep case five compile errors.
     */
    private static final Pattern CONSTRUCTION =
            Pattern.compile("\\bnew\\s*(?:[A-Za-z_][\\w.']*(?::[A-Za-z_][\\w']*)?\\s*)?[({]");

    /** An attachment: a listener declaration, or a service bound to one. */
    private static final Pattern ATTACHMENT = Pattern.compile("(?m)^\\s*(listener\\b|service\\b)");

    /** A call through a client, in either grammar: {@code ->name(…)} or {@code ->/path.accessor(…)}. */
    private static final Pattern CALL = Pattern.compile("->\\s*(/[^\\s(;]*|[A-Za-z_][A-Za-z0-9_]*)");

    /** A call by NAME, with the parenthesis that distinguishes it from a field read. */
    private static final Pattern CALL_BY_NAME =
            Pattern.compile("->\\s*([A-Za-z_][A-Za-z0-9_]*)\\s*\\(");

    /** A call by PATH. The trailing accessor is split off inside {@link #undeclaredInLine}. */
    private static final Pattern CALL_BY_PATH = Pattern.compile("->\\s*(/[^\\s(;]*)\\s*\\(");

    /** The accessor Ballerina assumes when a resource call names none. */
    private static final String DEFAULT_ACCESSOR = "get";

    private Snippets() {
    }

    /**
     * What a block demonstrates, and the order the section prints the roles in.
     *
     * <p>Role order rather than source order, because construction has to precede use — and because it is what
     * surfaces the right blocks for kafka, whose producer construction is block 1 and whose listener service is
     * block 3 with two consumer variants in between.
     */
    private enum Role {

        CONSTRUCTION,
        CALL,
        ATTACHMENT
    }

    /** The blocks {@code overview} prints, and how many eligible ones the budget left behind. */
    public record Usage(List<String> blocks, int omitted) {

        public boolean isEmpty() {
            return blocks.isEmpty();
        }
    }

    /** One candidate block: what it demonstrates, what makes it a duplicate, and its text. */
    private record Candidate(Role role, String key, String code) {

        int lines() {
            return code.strip().split("\n", -1).length;
        }
    }

    /**
     * The readme's worked code, ready to print.
     *
     * <p>Every module's guide contributes, in Central's order, because a package that publishes several publishes
     * one per module and the caller asked about the package.
     */
    public static Usage select(LoadedPackage loaded) {
        Vocabulary vocabulary = Vocabulary.of(loaded);
        List<Candidate> eligible = new ArrayList<>();
        for (Readmes.ModuleReadme readme : loaded.readmes()) {
            eligible.addAll(candidates(readme.markdown(), vocabulary));
        }
        List<Candidate> deduped = dedupe(eligible);
        List<Candidate> chosen = withinBudget(deduped);
        List<String> blocks = chosen.stream()
                .map(candidate -> annotate(candidate.code(), loaded, vocabulary))
                .toList();
        return new Usage(blocks, eligible.size() - chosen.size());
    }

    // -----------------------------------------------------------------------
    // Selection
    // -----------------------------------------------------------------------

    private static List<Candidate> candidates(String markdown, Vocabulary vocabulary) {
        List<Candidate> candidates = new ArrayList<>();
        for (Readmes.Block block : Readmes.blocks(markdown)) {
            if (!BALLERINA_FENCES.contains(block.language())) {
                continue;
            }
            String code = deindent(block.code());
            if (code.isEmpty()) {
                continue;
            }
            Role role = roleOf(code);
            if (role == null) {
                continue;
            }
            candidates.add(new Candidate(role, keyOf(role, code, vocabulary), code));
        }
        return candidates;
    }

    /**
     * Drop the indentation a fence inherited from the list item it was nested under.
     *
     * <p>{@code ballerinax/postgresql} indents most of its fences four spaces, which the block carries
     * verbatim — and the FIRST line of a stripped block loses it while the rest keep it, so the snippet arrives
     * looking like a fragment of something larger. The common prefix is layout rather than content, so removing
     * it is not editing the quotation: the relative indentation, which is the part that means something, is
     * untouched.
     */
    private static String deindent(String code) {
        List<String> lines = List.of(code.strip().split("\n", -1));
        int common = lines.stream()
                .filter(line -> !line.isBlank())
                .mapToInt(line -> line.length() - line.stripLeading().length())
                .min()
                .orElse(0);
        if (common == 0) {
            return String.join("\n", lines);
        }
        return lines.stream()
                .map(line -> line.length() >= common ? line.substring(common) : line.stripLeading())
                .collect(Collectors.joining("\n"));
    }

    /**
     * What a block demonstrates, or {@code null} when it demonstrates nothing callable.
     *
     * <p>The order of the tests is the priority, and one case decides it: kafka's listener example both
     * constructs and calls, and it is neither of those to a reader — it is the shape a consumer service takes.
     * A block that constructs AND calls is filed as a CONSTRUCTION so that the fuller block leads and a separate
     * call block still gets the call slot; filed the other way round, a quickstart that does both would take the
     * call slot and leave the construction slot empty.
     */
    private static Role roleOf(String code) {
        if (ATTACHMENT.matcher(code).find()) {
            return Role.ATTACHMENT;
        }
        if (CONSTRUCTION.matcher(code).find()) {
            return Role.CONSTRUCTION;
        }
        return CALL.matcher(code).find() ? Role.CALL : null;
    }

    /**
     * What makes two blocks the same example, which differs by role because the duplication does.
     *
     * <p>A CONSTRUCTION is keyed by the CLIENT it builds: postgresql's guide opens with three near-identical
     * {@code postgresql:Client … = new (…)} blocks — empty, positional and named — and printing all three spends
     * the budget saying one thing. A CALL is keyed by the OPERATION: {@code ->execute} and {@code ->query} are
     * two facts, not one, so keying calls by client would collapse a database connector to a single example.
     * An ATTACHMENT is keyed on nothing, because one is the shape and a second is a variation on it.
     */
    private static String keyOf(Role role, String code, Vocabulary vocabulary) {
        return switch (role) {
            case CONSTRUCTION -> constructedClient(code, vocabulary);
            case CALL -> firstCallTarget(code);
            case ATTACHMENT -> "";
        };
    }

    /** The first {@code alias:Name} in the block that names a client of this package. */
    private static String constructedClient(String code, Vocabulary vocabulary) {
        Matcher reference = vocabulary.ownReference().matcher(code);
        while (reference.find()) {
            if (vocabulary.clientNames().contains(reference.group(1))) {
                return reference.group(1);
            }
        }
        return "";
    }

    private static String firstCallTarget(String code) {
        Matcher call = CALL.matcher(code);
        return call.find() ? call.group(1) : "";
    }

    /**
     * One block per (role, key), and the FIRST one wins.
     *
     * <p>First rather than fullest, and the difference is measurable. A readme is ordered basic-first, so the
     * earliest block for a key is the canonical one and every later one is a variant. Taking the longest
     * instead picked {@code ballerinax/postgresql}'s eleven-line connection-POOL example over the plain
     * constructor its quickstart opens with — a specialised setup presented as the way to reach a database.
     */
    private static List<Candidate> dedupe(List<Candidate> candidates) {
        Map<String, Candidate> first = new LinkedHashMap<>();
        for (Candidate candidate : candidates) {
            first.putIfAbsent(candidate.role() + " " + candidate.key(), candidate);
        }
        List<Candidate> ordered = new ArrayList<>(first.values());
        ordered.sort(java.util.Comparator.comparing(Candidate::role));
        return ordered;
    }

    /**
     * Fill the budget with COVERAGE first, then depth.
     *
     * <p>One block of each role is taken before a second of any, and that is what the budget is for. Filling in
     * role order alone spends kafka's forty lines on two constructions and a send, and loses the listener
     * service — the block the 2026-08-15 sweep predicted would be the next thing an agent had to guess at.
     */
    private static List<Candidate> withinBudget(List<Candidate> candidates) {
        List<Candidate> chosen = new ArrayList<>();
        Set<Role> covered = new LinkedHashSet<>();
        int lines = 0;
        for (int pass = 0; pass < 2; pass++) {
            for (Candidate candidate : candidates) {
                if (chosen.contains(candidate)) {
                    continue;
                }
                if (pass == 0 && covered.contains(candidate.role())) {
                    continue;
                }
                if (lines + candidate.lines() > MAX_USAGE_LINES) {
                    continue;
                }
                chosen.add(candidate);
                covered.add(candidate.role());
                lines += candidate.lines();
            }
        }
        // Printed in role order regardless of which pass took them: construct, call, attach.
        List<Candidate> ordered = new ArrayList<>(chosen);
        ordered.sort(java.util.Comparator.comparing(Candidate::role));
        return ordered;
    }

    // -----------------------------------------------------------------------
    // Validation
    // -----------------------------------------------------------------------

    /**
     * Every name this package puts in a caller's reach, split by the grammar that reaches it.
     *
     * <p>Three universes rather than one, because the three call forms are checked against different things and
     * merging them would let a resource path validate a remote function name. {@code types} is what
     * {@code alias:Name} may be; {@code callables} is what {@code ->name(} may be; {@code paths} is what
     * {@code ->/segment.accessor(} may be.
     *
     * <p>The {@code callables} universe is the widening this class was built for. It used to be absent entirely —
     * the check covered {@code alias:Name} only — which is exactly why mongodb's {@code ->insert(movie)} passed
     * a validator that was looking at the {@code Movie} beside it, in a package declaring {@code insertOne} and
     * {@code insertMany} and no {@code insert}.
     */
    private record Vocabulary(
            List<String> types, List<String> callables, Set<String> clientNames, PathTree paths,
            Pattern ownReference) {

        static Vocabulary of(LoadedPackage loaded) {
            Library library = loaded.library();
            List<String> types = new ArrayList<>();
            for (TypeDef declaration : library.addressable()) {
                types.add(declaration.name());
                if (declaration instanceof TypeDef.Enumeration enumeration) {
                    types.addAll(enumeration.memberNames());
                }
            }
            library.annotations().forEach(annotation -> types.add(annotation.name()));
            library.functions().forEach(function -> types.add(function.name()));

            List<String> callables = new ArrayList<>(
                    library.functions().stream().map(Fn.Standalone::name).toList());
            Set<String> clients = new LinkedHashSet<>();
            List<PathTree.Operation> operations = new ArrayList<>();
            for (ClientClass client : library.clients()) {
                clients.add(client.name());
                callables.addAll(methodNames(client.functions()));
                operations.addAll(PathTree.operationsOf(client));
            }
            // A client declared `public type X client object` is not in `clients()` — SQL-03 — and its methods
            // are reached with `->` exactly the same way.
            for (TypeDef declaration : library.declarations()) {
                if (declaration instanceof TypeDef.ObjectDef object
                        && object.role() == TypeDef.ObjectDef.Role.CLIENT) {
                    clients.add(object.name());
                    callables.addAll(methodNames(object.methods()));
                }
            }
            // DE-DUPLICATED, and that is not tidiness. Two clients of one package routinely declare the same
            // operation — kafka's `Caller` and `Consumer` both declare `'commit` — and `Names.match` answers
            // AMBIGUOUS when two roster entries normalise alike, which is the right answer when a caller is
            // choosing a declaration to read and the wrong one when the question is only "does this exist".
            return new Vocabulary(
                    List.copyOf(new LinkedHashSet<>(types)),
                    List.copyOf(new LinkedHashSet<>(callables)),
                    Set.copyOf(clients),
                    PathTree.build(List.copyOf(operations)),
                    // Compiled once per lookup rather than once per line of quoted code.
                    Pattern.compile("\\b" + Pattern.quote(loaded.qualified().moduleAlias())
                            + ":([A-Z][A-Za-z0-9_]*)"));
        }

        private static List<String> methodNames(List<Fn> functions) {
            return functions.stream()
                    .filter(Fn.Standalone.class::isInstance)
                    .map(fn -> ((Fn.Standalone) fn).name())
                    .toList();
        }
    }

    /**
     * Mark the lines a snippet gets wrong, rather than dropping the block that carries them.
     *
     * <p>ADR-0010: a named gap beats a plausible guess. Dropping the block would also lose the construction
     * context around the bad line, and a reader who never sees the example cannot tell that the package's own
     * documentation is where the wrong name came from. The mark sits directly above the line, as a Ballerina doc
     * comment, so it survives a copy-paste into an editor as a comment rather than as a syntax error.
     */
    private static String annotate(String code, LoadedPackage loaded, Vocabulary vocabulary) {
        List<String> annotated = new ArrayList<>();
        for (String line : code.split("\n", -1)) {
            String note = undeclaredInLine(line, loaded, vocabulary);
            if (note != null) {
                annotated.add(line.substring(0, line.length() - line.stripLeading().length()) + note);
            }
            annotated.add(line);
        }
        return String.join("\n", annotated);
    }

    /** The first thing on this line that this version does not declare, as a doc comment, or {@code null}. */
    private static String undeclaredInLine(String line, LoadedPackage loaded, Vocabulary vocabulary) {
        Matcher byName = CALL_BY_NAME.matcher(line);
        while (byName.find()) {
            String name = byName.group(1);
            if (Names.match(name, vocabulary.callables()) instanceof Names.Match.Missing) {
                return mark(name, "an operation", Names.nearMisses(name, vocabulary.callables()), loaded);
            }
        }
        Matcher byPath = CALL_BY_PATH.matcher(line);
        while (byPath.find()) {
            String target = byPath.group(1);
            if (!resolves(target, vocabulary.paths())) {
                return mark(target, "a resource path", List.of(), loaded);
            }
        }
        Matcher reference = vocabulary.ownReference().matcher(line);
        while (reference.find()) {
            String name = reference.group(1);
            if (Names.match(name, vocabulary.types()) instanceof Names.Match.Missing) {
                return mark(name, "a declaration", Names.nearMisses(name, vocabulary.types()), loaded);
            }
        }
        return null;
    }

    private static String mark(String name, String kind, List<String> candidates, LoadedPackage loaded) {
        // The full candidate list, not a trimmed one. Measured on gmail: the right answer for
        // `MessageListPage` is `ListMessagesResponse`, and it ranks fifth — a three-name line loses exactly the
        // name the mark exists to supply, which is worse than a long line.
        String closest = candidates.isEmpty()
                ? ""
                : " — closest: " + candidates.stream().distinct().limit(Names.MAX_CANDIDATES)
                        .map(Texts::code).collect(Collectors.joining(", "));
        return "# ⚠ " + Texts.code(name) + " is not " + kind + " " + loaded.qualified().qualified()
                + " " + loaded.version().text() + " declares" + closest + ".";
    }

    /**
     * Does a written resource call address something this package has?
     *
     * <p>Deliberately tolerant in one direction only. A readme writes CONCRETE values where the tree holds path
     * PARAMETERS — {@code ->/repos/["ballerina"]/…} against {@code repos/{owner}} — so a segment that matches no
     * literal child is retried as a wildcard, which is the same token {@code ops} accepts. What is not tolerated
     * is a first segment or an accessor the package never declares, which is the class of staleness worth
     * flagging.
     */
    private static boolean resolves(String target, PathTree paths) {
        String accessor = DEFAULT_ACCESSOR;
        String pathPart = target;
        int split = lastUnescapedDot(target);
        if (split > 0) {
            accessor = target.substring(split + 1);
            pathPart = target.substring(0, split);
        }
        PathTree node = paths;
        int depth = 0;
        for (String written : PathTree.splitPath(pathPart)) {
            String readable = PathTree.readableSegment(written);
            PathTree scope = node;
            boolean literal = scope.children().stream()
                    .anyMatch(child -> child.segment().equals(readable) || child.segment().equals(written));
            PathTree next = scope.children().stream()
                    .filter(child -> literal
                            ? child.segment().equals(readable) || child.segment().equals(written)
                            : child.isParam())
                    .findFirst()
                    .orElse(null);
            if (next == null) {
                return false;
            }
            node = next;
            depth++;
        }
        if (depth == 0) {
            return false;
        }
        String called = accessor;
        return node.operations().stream().anyMatch(operation -> operation.fn().accessor().equals(called));
    }

    /** The accessor separator. A dot inside an identifier is escaped, which is how slack's paths are spelled. */
    private static int lastUnescapedDot(String target) {
        for (int index = target.length() - 1; index >= 0; index--) {
            if (target.charAt(index) == '.' && (index == 0 || target.charAt(index - 1) != '\\')) {
                return index;
            }
        }
        return -1;
    }

    // -----------------------------------------------------------------------
    // The whole-readme warning, for the verb that prints the whole readme
    // -----------------------------------------------------------------------

    /**
     * Names the readme's own examples use that this package does not declare, as one paragraph.
     *
     * <p>GMAIL-03. gmail's quickstart — the only worked example in any of its four documents, and where an agent
     * starts — declares {@code gmail:MessageListPage}, a name that occurs exactly once in the whole 700KB
     * payload: inside that readme. The source declares {@code ListMessagesResponse}; the readme is stale from an
     * earlier major version. So the same tool told the reader both that this is the type to declare and (from
     * {@code type}, {@code api} and {@code overview}'s counts) that no such declaration exists.
     *
     * <p>This is the whole-document form, and it belongs to {@code guide} because {@code guide} is what
     * reproduces the whole document. Inside {@code overview}, where only selected blocks are quoted, the same
     * facts are printed by {@link #annotate} on the line that carries them — which is better timed and does not
     * ask the reader to hold a list of names while scanning for them.
     */
    public static String staleNames(String markdown, LoadedPackage loaded) {
        Vocabulary vocabulary = Vocabulary.of(loaded);
        Set<String> missing = new LinkedHashSet<>();
        for (Readmes.Block block : Readmes.blocks(markdown)) {
            Matcher matcher = vocabulary.ownReference().matcher(block.code());
            while (matcher.find()) {
                String name = matcher.group(1);
                if (Names.match(name, vocabulary.types()) instanceof Names.Match.Missing) {
                    missing.add(name);
                }
            }
            Matcher byName = CALL_BY_NAME.matcher(block.code());
            while (byName.find()) {
                String name = byName.group(1);
                if (Names.match(name, vocabulary.callables()) instanceof Names.Match.Missing) {
                    missing.add(name);
                }
            }
        }
        if (missing.isEmpty()) {
            return null;
        }
        List<String> candidates = missing.stream()
                .flatMap(name -> java.util.stream.Stream.concat(
                        Names.nearMisses(name, vocabulary.types()).stream(),
                        Names.nearMisses(name, vocabulary.callables()).stream()))
                .distinct()
                .limit(Names.MAX_CANDIDATES)
                .toList();
        return "**This guide is reproduced as the package published it, and "
                + (missing.size() == 1 ? "one name in it is" : missing.size() + " names in it are")
                + " not declared by this package** — "
                + missing.stream().map(Texts::code).collect(Collectors.joining(", "))
                + ". Code copied from below will not compile against "
                + Texts.code(loaded.qualified().qualified()) + " " + loaded.version().text()
                + (candidates.isEmpty()
                        ? "."
                        : "; the closest declared names are "
                                + candidates.stream().map(Texts::code).collect(Collectors.joining(", "))
                                + ".");
    }
}
