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

package io.ballerina.library.cli;

import picocli.CommandLine;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The argument grammar and its one-line prose, declared once.
 *
 * <p>This is the one place the port is deliberately BETTER than its TypeScript source rather than faithful to it.
 * There, "a flag a verb does not take is a failure, not a silently dropped argument" cost about sixty lines across
 * four hand-maintained structures. Here it is a consequence of where the field is declared: {@code --module}
 * exists on {@link Guide} and nowhere else, so {@code overview --module} is an unmatched argument before any of
 * our code runs.
 *
 * <p>Every {@code description} here is written ONCE and rendered into BOTH the root usage text and the verb's own
 * {@code --help} by {@link UsageRenderer} (ADR-0012). They used to be two strings that said nearly the same thing,
 * and the pair had already drifted: the root synopsis advertised {@code --client C} while the flag list below it
 * and every verb text said {@code --client <Name>}.
 *
 * <p><b>WHAT IS NOT HERE.</b> Three flags were deleted rather than renamed, and one is hidden:
 *
 * <ul>
 *   <li>{@code --version} and {@code --project-dir} are gone because version resolution is INTERNAL now
 *       (§3.8). The tool walks up from the process's directory for a {@code Ballerina.toml} and reads the
 *       {@code Dependencies.toml} beside it, which carries the TRANSITIVE closure — measured,
 *       {@code maintenance_api} imports 8 packages directly and locks 36 — so a caller never has a version
 *       question to answer and no document discloses the resolution.
 *   <li>{@code --client} is gone because a container is a POSITIONAL now: {@code client <pkg> Client} reads the
 *       way every other addressed argument does.
 *   <li>{@code --all} is declared {@code hidden}. It is an escape hatch, not part of the taught contract, so it
 *       does not appear in {@code --help} at all; the documents that collapse a section offer it in their
 *       {@code ## Next} block, last, with the byte cost stated (ADR-0013).
 * </ul>
 *
 * <p>NO ALIASES for the removed names. Every consumer is ours — the tool is not on Central, so consumers copy it
 * — and {@code ops} cannot be aliased correctly in any case, because it splits three ways.
 *
 * <p>All eight classes are argument holders with no logic. Dispatch is {@link Cli}'s job, so that the exit-code
 * mapping and the stream discipline stay in one readable method instead of being spread across eight
 * {@code Runnable}s.
 *
 * @since 0.1.0
 */
final class Commands {

    private Commands() {
    }

    /**
     * The parser and the holders it fills.
     *
     * <p>One factory rather than two assembly sites: {@link Cli} needs it to dispatch and {@link LibraryTool}
     * needs it to answer {@code bal help library}, and a second {@code addSubcommand} chain is a second place a
     * verb can be forgotten.
     */
    record Grammar(
            CommandLine line,
            Find find,
            Overview overview,
            Client client,
            Klass klass,
            Funcs funcs,
            Type type,
            Guide guide,
            Api api) {

        static Grammar create() {
            Find find = new Find();
            Overview overview = new Overview();
            Client client = new Client();
            Klass klass = new Klass();
            Funcs funcs = new Funcs();
            Type type = new Type();
            Guide guide = new Guide();
            Api api = new Api();
            CommandLine line = new CommandLine(new Root())
                    .addSubcommand("find", find)
                    .addSubcommand("overview", overview)
                    .addSubcommand("client", client)
                    .addSubcommand("class", klass)
                    .addSubcommand("funcs", funcs)
                    .addSubcommand("type", type)
                    .addSubcommand("guide", guide)
                    .addSubcommand("api", api);
            return new Grammar(line, find, overview, client, klass, funcs, type, guide, api);
        }

        /** The verbs, in the order they were registered — which is the order the usage text lists them. */
        List<String> verbs() {
            return List.copyOf(line.getSubcommands().keySet());
        }

        CommandLine.Model.CommandSpec verb(String name) {
            return line.getSubcommands().get(name).getCommandSpec();
        }

        /**
         * Which verbs would have accepted a flag, derived from where the field is declared.
         *
         * <p>For the MESSAGE only — the rejection itself is structural. Without it, {@code overview -r} reports
         * "Unknown option '-r'", which is true and unhelpful; with it, the suggestion names the verbs that take
         * it. This was a hand-written {@code Map} whose entries no test compared against the annotations, so a
         * flag moved between verbs would have kept advertising its old owner.
         */
        Map<String, List<String>> flagOwners() {
            Map<String, List<String>> owners = new LinkedHashMap<>();
            for (String verb : verbs()) {
                for (CommandLine.Model.OptionSpec option : verb(verb).options()) {
                    if (option.usageHelp() || option.hidden()) {
                        continue;
                    }
                    // Keyed on EVERY spelling, not only the long one. A caller who typed `-r` gets "Unknown
                    // option" from a map keyed on `--resolve-types` alone — true, unhelpful, and exactly the
                    // guess-the-owner message this map exists to replace.
                    for (String name : option.names()) {
                        owners.computeIfAbsent(name, key -> new ArrayList<>()).add(verb);
                    }
                }
            }
            return owners;
        }

        /** The long form of a flag, whichever spelling was typed. */
        String canonicalFlag(String name) {
            for (String verb : verbs()) {
                for (CommandLine.Model.OptionSpec option : verb(verb).options()) {
                    if (List.of(option.names()).contains(name)) {
                        return option.longestName();
                    }
                }
            }
            return name;
        }
    }

    /**
     * The verbs whose variadic slot must not be empty.
     *
     * <p>ONE declaration, read by two places: {@link UsageRenderer} brackets every other variadic slot, and
     * {@link Cli} rejects an empty list for these. It has to be declared rather than derived because picocli cannot
     * express it — a positional with a minimum arity consumes an unrecognised FLAG as its value on 4.0.1 (see
     * {@link Find#keywords}), which is the silent class this grammar refuses everywhere else. So the requirement
     * lives here, beside the grammar it belongs to, instead of being hand-written into the synopsis and the
     * validator separately and drifting the way {@code --client}'s label already did once.
     *
     * <p>{@code type} is on the list even though it also accepts {@code -s} instead of a name: a bare
     * {@code type <pkg>} must not become a second {@code api}, and the synopsis showing the name as required is
     * the honest summary of "one of these two". {@link Cli} states the disjunction in the failure text.
     */
    static final List<String> REQUIRES_POSITIONALS = List.of("find", "type");

    /**
     * Every verb answers {@code --help}, including the one that reads no package.
     *
     * <p>{@code usageHelp} is what lets a required positional coexist with a help request: picocli skips the
     * required-argument check when this flag is present, so {@code bal library overview --help} is a usage
     * request rather than "missing {@code <org/name>}".
     */
    static final class Help {

        @CommandLine.Option(names = {"-h", "--help"}, usageHelp = true, description = "This text.")
        boolean help;
    }

    /**
     * The one resolution knob left, on the verbs that READ A PACKAGE.
     *
     * <p>Deliberately not mixed into {@code find}: a registry query resolves no version and is never cached, so
     * {@code --refresh} would be accepted there and do nothing. A flag that parses and is then dropped is the
     * silent class of mistake this grammar refuses everywhere else.
     */
    static final class Resolution {

        @CommandLine.Mixin
        Help help = new Help();

        @CommandLine.Option(names = "--refresh",
                description = "Ignore any cached copy and rewrite it. Worth passing only when a name should "
                        + "exist and does not.")
        boolean refresh;
    }

    /**
     * {@code -s}, on every verb that lists something.
     *
     * <p>A mixin rather than a field on each, so the flag's prose cannot say one thing on {@code client} and
     * another on {@code overview} — which is exactly what the two hand-written help texts used to do for
     * {@code --client}. What each verb SEARCHES OVER differs, and that belongs in the verb's own note.
     */
    static final class Search {

        @CommandLine.Option(names = {"-s", "--search"}, paramLabel = "<q>",
                description = "Filter this verb's scope by name, path, parameter or type. Every word and every "
                        + "path segment of the query has to appear, in any order — to anchor a path, pass it as "
                        + "the selector instead. A match found only in documentation is named rather than "
                        + "printed.")
        String query;
    }

    /**
     * {@code -r} and {@code --all}, on the four verbs that print declarations.
     *
     * <p>They ride together because they are the two answers to "that was not enough": one goes DEEPER — the
     * types a signature names, transitively — and the other goes WIDER, ignoring the byte budget. Only the first
     * is taught.
     */
    static final class Depth {

        @CommandLine.Option(names = {"-r", "--resolve-types"},
                description = "Answer as Ballerina: the declaration, then the transitive closure of every type "
                        + "it names, with cross-package edges named rather than followed.")
        boolean resolve;

        // Hidden, and offered only by a document that collapsed a section — with its byte cost, beside the thing
        // it would expand. See the class comment.
        @CommandLine.Option(names = "--all", hidden = true,
                description = "Ignore the byte budget and print every signature at this scope.")
        boolean all;
    }

    /**
     * The root. It takes no positionals of its own, which is the deliberate departure from the Node CLI: there, a
     * first positional containing {@code /} was a package and the verb defaulted to {@code overview}. Under
     * {@code bal}, {@code bal library ballerinax/github} reads as a subcommand typo, and
     * {@code bal library overview ballerinax/github} costs one token to be unambiguous.
     */
    @CommandLine.Command(name = "library")
    static final class Root {

        @CommandLine.Mixin
        Help help = new Help();
    }

    @CommandLine.Command(name = "find",
            description = "Packages matching free-text keywords, one line each, in Central's relevance order "
                    + "with pull counts. The only verb that needs no package name.")
    static final class Find {

        @CommandLine.Mixin
        Help help = new Help();

        /**
         * Deliberately NOT {@code arity = "1..*"}, which would let picocli enforce it.
         *
         * <p>Measured on 4.0.1: a variable-arity positional with a minimum consumes the tokens after it
         * INCLUDING an unrecognised flag, so {@code find kafka -r} parses with {@code -r} as a keyword instead
         * of being rejected. That is the silent class this grammar refuses everywhere else, and it is worth an
         * emptiness check in {@link Cli} to keep the rejection structural.
         */
        @CommandLine.Parameters(index = "0..*", paramLabel = "<keywords>")
        List<String> keywords;
    }

    @CommandLine.Command(name = "overview",
            description = "A map of the package: what it declares, the readme's worked code, and the "
                    + "command that opens each part. Start here.")
    static final class Overview {

        // Declaration order IS synopsis order: the flags this verb alone takes come before the shared resolution
        // ones, so `bal library overview --help` leads with what distinguishes it.
        @CommandLine.Mixin
        Search search = new Search();

        @CommandLine.Mixin
        Resolution resolution = new Resolution();

        @CommandLine.Parameters(index = "0", arity = "1", paramLabel = "<org/name>")
        String pkg;
    }

    @CommandLine.Command(name = "client",
            description = "One client's whole callable surface, or a roster of them. Resource functions by "
                    + "path, remote and normal ones by name, the constructor included.")
    static final class Client {

        @CommandLine.Mixin
        Search search = new Search();

        @CommandLine.Mixin
        Depth depth = new Depth();

        @CommandLine.Mixin
        Resolution resolution = new Resolution();

        @CommandLine.Parameters(index = "0", arity = "1", paramLabel = "<org/name>")
        String pkg;

        /**
         * One slot for the container AND its selector, because which is which is a property of the PACKAGE.
         *
         * <p>A caller cannot know whether a connector is path-shaped or name-shaped until they look, so a second
         * positional or a mode flag would be asking for the answer as the question. {@link Cli} resolves the
         * container first and then parses what is left against what that container declares.
         */
        @CommandLine.Parameters(index = "1..*", paramLabel = "<Name|selector>")
        List<String> selectors;
    }

    @CommandLine.Command(name = "class",
            description = "A class, service type or listener, and its members. Same shape as 'client'; these "
                    + "are the objects called with '.' rather than '->'.")
    static final class Klass {

        @CommandLine.Mixin
        Search search = new Search();

        @CommandLine.Mixin
        Depth depth = new Depth();

        @CommandLine.Mixin
        Resolution resolution = new Resolution();

        @CommandLine.Parameters(index = "0", arity = "1", paramLabel = "<org/name>")
        String pkg;

        @CommandLine.Parameters(index = "1..*", paramLabel = "<Name|member>")
        List<String> selectors;
    }

    @CommandLine.Command(name = "funcs",
            description = "Functions at module scope — the callable surface of a package that has no client, and "
                    + "the utilities of one that does.")
    static final class Funcs {

        @CommandLine.Mixin
        Search search = new Search();

        @CommandLine.Mixin
        Depth depth = new Depth();

        @CommandLine.Mixin
        Resolution resolution = new Resolution();

        @CommandLine.Parameters(index = "0", arity = "1", paramLabel = "<org/name>")
        String pkg;

        @CommandLine.Parameters(index = "1..*", paramLabel = "<name|prefix*>")
        List<String> selectors;
    }

    @CommandLine.Command(name = "type",
            description = "One or more declarations, as Ballerina. All-or-nothing: a name that does not resolve "
                    + "fails the run with candidates.")
    static final class Type {

        @CommandLine.Mixin
        Search search = new Search();

        @CommandLine.Mixin
        Depth depth = new Depth();

        @CommandLine.Mixin
        Resolution resolution = new Resolution();

        @CommandLine.Parameters(index = "0", arity = "1", paramLabel = "<org/name>")
        String pkg;

        /** No minimum arity, for the reason {@link Find#keywords} records: it would swallow a foreign flag. */
        @CommandLine.Parameters(index = "1..*", paramLabel = "<Name>")
        List<String> names;
    }

    @CommandLine.Command(name = "guide",
            description = "The package's own readme, verbatim: setup steps, account prerequisites and worked "
                    + "recipes. Addressable one chunk at a time.")
    static final class Guide {

        @CommandLine.Option(names = "--module", paramLabel = "<name>",
                description = "One module's readme. A package publishes one per module, and this verb's own "
                        + "'Modules' row names them.")
        String module;

        @CommandLine.Mixin
        Search search = new Search();

        @CommandLine.Mixin
        Resolution resolution = new Resolution();

        @CommandLine.Parameters(index = "0", arity = "1", paramLabel = "<org/name>")
        String pkg;

        @CommandLine.Parameters(index = "1", arity = "0..1", paramLabel = "<n|title>")
        String chunk;
    }

    @CommandLine.Command(name = "api",
            // "the other verbs" rather than "the ones above", because this string renders into two layouts: the
            // root list, where they ARE above, and this verb's own page, where nothing is.
            description = "Last resort. Every declaration in one document, unaddressed and large — reach for it "
                    + "only when the other verbs did not answer.")
    static final class Api {

        @CommandLine.Mixin
        Resolution resolution = new Resolution();

        @CommandLine.Parameters(index = "0", arity = "1", paramLabel = "<org/name>")
        String pkg;
    }
}
