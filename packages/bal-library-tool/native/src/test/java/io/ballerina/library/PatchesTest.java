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

import io.ballerina.library.model.Library;
import io.ballerina.library.model.Patches;
import io.ballerina.library.model.RecordField;
import io.ballerina.library.model.Service;
import io.ballerina.library.model.TypeDef;
import org.testng.Assert;
import org.testng.annotations.Test;

import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The per-package corrections, pinned against the fixtures they correct.
 *
 * <p>A patch is a hand-maintained claim about a package, which is exactly the kind of thing that rots silently:
 * Central starts publishing the field, or the package renames the type, and the correction goes on overriding
 * reality. So each one is pinned in BOTH directions where it has a negative case — what it must change, and what
 * it must leave alone.
 *
 * @since 0.1.0
 */
public class PatchesTest {

    private static Map<String, TypeDef.ErrorDef> errorsOf(String slug) {
        Map<String, TypeDef.ErrorDef> errors = new LinkedHashMap<>();
        for (TypeDef typeDef : FixtureCorpus.libraryFor(slug).typeDefs()) {
            if (typeDef instanceof TypeDef.ErrorDef error) {
                errors.put(error.name(), error);
            }
        }
        return errors;
    }

    private static String baseOf(TypeDef.ErrorDef error) {
        return error.base().map(base -> base.name()).orElse(null);
    }

    /**
     * Every declaration's type descriptor as rendered, by name — errors by their base, aliases by their
     * type.
     *
     * <p>Both, because the detail-argument correction spans both: Central files three of http's five under
     * {@code errors} and two under {@code intersectionTypes}, and a test that looked only at
     * {@link TypeDef.ErrorDef} would report full coverage while missing 2 of 11 sites.
     */
    private static Map<String, String> descriptorsOf(String slug) {
        Map<String, String> descriptors = new LinkedHashMap<>();
        for (TypeDef typeDef : FixtureCorpus.libraryFor(slug).typeDefs()) {
            if (typeDef instanceof TypeDef.ErrorDef error) {
                descriptors.put(error.name(), baseOf(error));
            } else if (typeDef instanceof TypeDef.Alias alias) {
                descriptors.put(alias.name(), alias.type().name());
            }
        }
        return descriptors;
    }

    /**
     * The whole detail-argument table, every row, in the direction that catches a rot.
     *
     * <p>This is the pin the mechanism went without for as long as it was silent. The correction matches a
     * RENDERED intersection, so a change to how {@code &} is spaced — or to which IR case a category becomes
     * — stops it matching, and a patch that matches nothing produces no failure of its own. Sampling one row
     * does not help: the version of this that asserted a reachable {@code Detail} record stayed green while
     * eight of the eleven sites went uncorrected, two of them in http itself.
     */
    @Test
    public void everyDeclarationInTheDetailArgumentTableGetsItsArgument() {
        Map<String, Map<String, String>> expected = Map.of(
                "ballerina__http", Map.of(
                        "ApplicationResponseError", "(ClientError & error<Detail>)",
                        "ClientRequestError", "(ApplicationResponseError & error<Detail>)",
                        "RemoteServerError", "(ApplicationResponseError & error<Detail>)",
                        "LoadBalanceActionError", "distinct ResiliencyError & error<LoadBalanceActionErrorData>",
                        "StatusCodeResponseBindingError",
                        "distinct ClientError & error<StatusCodeBindingErrorDetail>"),
                "ballerinax__kafka", Map.of(
                        "PayloadBindingError", "(Error & error<PartitionOffset>)",
                        "PayloadValidationError", "(PayloadBindingError & error<PartitionOffset>)"),
                "ballerina__graphql", Map.of(
                        "HttpError", "(RequestError & error<record {| anydata body; |}>)",
                        "InvalidDocumentError", "(RequestError & error<record {| ErrorDetail[]? errors; |}>)",
                        "PayloadBindingError", "(ClientError & error<record {| ErrorDetail[]? errors; |}>)",
                        "ServerError", "(ClientError & error<record {| json? data?; ErrorDetail[] errors; "
                                + "map<json>? extensions?; |}>)"));

        int rows = 0;
        for (Map.Entry<String, Map<String, String>> library : expected.entrySet()) {
            Map<String, String> descriptors = descriptorsOf(library.getKey());
            for (Map.Entry<String, String> row : library.getValue().entrySet()) {
                Assert.assertEquals(descriptors.get(row.getKey()), row.getValue(),
                        library.getKey() + " " + row.getKey() + " lost its detail argument");
                rows++;
            }
        }
        // Eleven across three packages, not five across two. The count is asserted so that deleting a row
        // from the table cannot pass as "every row still lands".
        Assert.assertEquals(rows, 11);
    }

    @Test
    public void anIntersectionOfTwoNamedErrorsIsLeftAlone() {
        // The over-reach guard. Widening the correction to aliases and to the unparenthesised spelling put
        // these two within range of a rule keyed on shape alone: they are unparenthesised intersection
        // aliases in the same category as `LoadBalanceActionError`, and their members are two named errors
        // rather than a bare `error`, so their sources give them no argument to restore. The declaration-name
        // gate is what excludes them.
        Map<String, String> descriptors = descriptorsOf("ballerina__http");
        Assert.assertEquals(descriptors.get("StatusCodeBindingClientRequestError"),
                "distinct StatusCodeResponseBindingError & ClientRequestError");
        Assert.assertEquals(descriptors.get("StatusCodeBindingRemoteServerError"),
                "distinct StatusCodeResponseBindingError & RemoteServerError");
    }

    @Test
    public void anErrorWhoseBaseIsAPlainReferenceIsLeftAlone() {
        // The negative half of the pin. Attaching `Detail` at the root `Error` is the intuitive reading of
        // Central's `detailType`, and it would make roughly 50 of http's 56 errors advertise an `int statusCode`
        // they do not have.
        Map<String, TypeDef.ErrorDef> errors = errorsOf("ballerina__http");
        for (String name : new String[] {"SslError", "ListenerError", "ClientError", "AllRetryAttemptsFailed"}) {
            TypeDef.ErrorDef error = errors.get(name);
            Assert.assertNotNull(error, name + " should be published as an error");
            String base = error.base().map(ref -> ref.name()).orElse("error");
            Assert.assertFalse(base.contains("Detail"), name + " must not mention Detail");
        }
        Assert.assertTrue(errors.get("Error").base().isEmpty(), "the root error narrows nothing");
    }

    @Test
    public void clientRequestErrorsChainReachesDetailInTheSamePackage() {
        // Following the chain rather than asserting one line: this is the lookup the recorded corpus came for,
        // and it is only useful if every hop resolves.
        Map<String, TypeDef.ErrorDef> errors = errorsOf("ballerina__http");
        Assert.assertEquals(baseOf(errors.get("ClientRequestError")),
                "(ApplicationResponseError & error<Detail>)");
        Assert.assertEquals(baseOf(errors.get("ApplicationResponseError")), "(ClientError & error<Detail>)");
        Assert.assertEquals(baseOf(errors.get("ClientError")), "Error");
        Assert.assertTrue(errors.get("Error").isDistinct());

        TypeDef.Rec detail = FixtureCorpus.libraryFor("ballerina__http").typeDefs().stream()
                .filter(TypeDef.Rec.class::isInstance)
                .map(TypeDef.Rec.class::cast)
                .filter(record -> record.name().equals("Detail"))
                .findFirst()
                .orElse(null);
        Assert.assertNotNull(detail);
        Assert.assertEquals(detail.fields().stream().map(RecordField::name).toList(),
                List.of("statusCode", "headers", "body"));
    }

    @Test
    public void everyErrorCarriesTheDistinctnessItDeclared() {
        // Including the one that declares none. The whole point of promoting the field: before this, all 74 error
        // declarations across the corpus rendered identically as `type X error;`.
        Map<String, TypeDef.ErrorDef> errors = errorsOf("ballerina__http");
        Assert.assertEquals(errors.size(), 56);
        List<TypeDef.ErrorDef> plain = errors.values().stream()
                .filter(error -> !error.isDistinct())
                .toList();
        // 55 of http's 56 are distinct, which is what keeps the non-distinct half of the rendering table from
        // being dead code nobody has ever exercised.
        Assert.assertEquals(plain.stream().map(TypeDef.ErrorDef::name).toList(),
                List.of("StatusCodeResponseDataBindingError"));
        // And it is not distinct for a reason worth keeping visible: it is an alias for a union of three other
        // errors, which is exactly the shape `distinct` cannot apply to.
        Assert.assertEquals(baseOf(plain.get(0)),
                "MediaTypeBindingStatusCodeClientError|PayloadBindingStatusCodeClientError"
                        + "|HeaderBindingStatusCodeClientError");
    }

    @Test
    public void sapDeclaresClientErrorExactlyOnceAndAsTheReExportItIs() {
        // SAP-01. `simpleNameReferenceTypes` carried `http:ClientError` all along; once that category is read,
        // the injection is skipped and the declaration is the real re-export rather than a bare `error`.
        // Exactly one either way: a duplicate is an ambiguity a name-addressed lookup cannot arbitrate.
        List<TypeDef> named = FixtureCorpus.libraryFor("ballerinax__sap").typeDefs().stream()
                .filter(typeDef -> typeDef.name().equals("ClientError"))
                .toList();
        Assert.assertEquals(named.size(), 1);
        Assert.assertTrue(named.get(0) instanceof TypeDef.Alias, "the re-export, not the injection");
        Assert.assertEquals(((TypeDef.Alias) named.get(0)).type().name(), "http:ClientError");
    }

    @Test
    public void sapDeclaresTheFourNamesItPublishesAndNoPhantom() {
        // SAP-02. `RequestMessage` used to be injected here, on the premise that sap re-exports it and
        // Central omits it. Neither half held: sap publishes four declarations and this is not one of them,
        // its own `.bal` files declare no such type, and all eight use sites are `ballerina/http`-qualified
        // already. The injection rendered as `// Unknown type: RequestMessage` — a placeholder standing in
        // for a name that is not sap's to declare.
        List<String> declared = FixtureCorpus.libraryFor("ballerinax__sap").declarations().stream()
                .map(TypeDef::name)
                .sorted()
                .toList();
        Assert.assertEquals(declared, List.of("CSRFTokenFetchFailure", "ClientError", "TargetType"));
        // And the name still reaches the caller, at every site that mentions it, qualified to the package
        // that owns it — which is the strictly-true statement the injection replaced with a false one.
        String document = FixtureCorpus.readSnapshot("ballerinax__sap");
        Assert.assertFalse(document.contains("// Unknown type:"));
        Assert.assertEquals(document.split("http:RequestMessage", -1).length - 1, 8);
    }

    /**
     * SLACK-15. The declaration Central omits is injected; the 179 references to it are left as the source
     * writes them.
     *
     * <p>Pinned in both directions because the previous repair went the other way — it rewrote the 179 field
     * types to the literal {@code true} and asserted the name was ABSENT from the document. That assertion
     * was green while the output diverged from slack's own source at 179 lines and denied the existence of a
     * public declaration.
     */
    @Test
    public void slackDeclaresOkTrueDefOnceAndEveryReferenceResolvesToIt() {
        String document = FixtureCorpus.readSnapshot("ballerinax__slack");
        Assert.assertEquals(document.split("(?m)^public type OkTrueDef true;$", -1).length - 1, 1,
                "exactly one declaration: a duplicate is an ambiguity a name-addressed lookup cannot arbitrate");
        // Every reference, at the spelling `types.bal` uses. The count is asserted so a reader change that
        // silently dropped the field type could not pass.
        Assert.assertEquals(document.split("(?m)^    OkTrueDef ok;$", -1).length - 1, 179);
        Assert.assertFalse(document.contains("    true ok;"), "the source writes the alias, not the literal");
        // And the point of injecting rather than erasing: the name is addressable.
        TypeDef declared = FixtureCorpus.libraryFor("ballerinax__slack").typeDefs().stream()
                .filter(typeDef -> "OkTrueDef".equals(typeDef.name()))
                .findFirst()
                .orElse(null);
        Assert.assertNotNull(declared, "`type ballerinax/slack OkTrueDef` must resolve");
        Assert.assertEquals(((TypeDef.Alias) declared).type().name(), "true");
        // No description, because `types.bal:1146` carries no doc comment and inventing one would be the same
        // category of error as the erasure this replaced.
        Assert.assertEquals(declared.description(), "");
    }

    /**
     * SHEETS-01, and what is left of {@code fixSheets2dArray} now that the patch is gone.
     *
     * <p>The patch hard-coded this one type on the premise that "Central types it one dimension short". The
     * premise named the wrong party: the payload carries {@code arrayDimensions: 2} and
     * {@code isParenthesisedType} on the element, and it was the READER that dropped a dimension. Once
     * dimensions are read, all three of the package's 2D value fields come out right — this one, which the
     * patch covered, and {@code ValuesRange.values} plus {@code appendValues}' parameter, which it never
     * touched and which are therefore the control.
     *
     * <p>Keyed on the field's NAME rather than on index 1, which is what the patch was keyed on: a positional
     * assumption is a claim about Central's field ORDER, which is not the fact under test here.
     */
    @Test
    public void sheetsReadsTheSecondArrayDimensionAtAllThreeSites() {
        TypeDef.Rec range = FixtureCorpus.libraryFor("ballerinax__googleapis.sheets").typeDefs().stream()
                .filter(TypeDef.Rec.class::isInstance)
                .map(TypeDef.Rec.class::cast)
                .filter(record -> record.name().equals("Range"))
                .findFirst()
                .orElse(null);
        Assert.assertNotNull(range);
        Assert.assertEquals(
                range.fields().stream()
                        .filter(field -> "values".equals(field.name()))
                        .map(field -> field.type().name())
                        .toList(),
                List.of("(int|string|decimal)[][]"));
        // The two the patch never reached, so a regression in the reader cannot hide behind a correction.
        String document = FixtureCorpus.readSnapshot("ballerinax__googleapis.sheets");
        Assert.assertTrue(document.contains("(int|string|decimal|boolean|float)[][] values;"));
        Assert.assertTrue(document.contains("(int|string|decimal|boolean|float)[][] values,"));
    }

    /**
     * What is left of {@code simplifyGraphQlErrorDetail}, which is deleted.
     *
     * <p>The patch rewrote {@code ErrorDetail.locations} because Central resolved it through the GraphQL
     * parser's internal {@code Location} type. It found that field by name among the members spliced in from
     * an inclusion — and once the inclusion renders as {@code *parser:ErrorDetail;}, which is byte-for-byte
     * what {@code records.bal:137-139} declares, there is no {@code locations} member to rewrite. Confirmed
     * inert before deletion: 0 of graphql's patched lines were its.
     *
     * <p>So the assertion is about the declaration being right rather than about a patch working — which is
     * the direction every one of these should have pointed in the first place. A test that asserts the
     * OUTCOME lets an inert correction sit undetected for as long as the reader happens to produce the same
     * answer, and two of them did.
     */
    @Test
    public void graphqlErrorDetailIsTheInclusionItsSourceDeclares() {
        TypeDef.Rec detail = FixtureCorpus.libraryFor("ballerina__graphql").typeDefs().stream()
                .filter(TypeDef.Rec.class::isInstance)
                .map(TypeDef.Rec.class::cast)
                .filter(record -> record.name().equals("ErrorDetail"))
                .findFirst()
                .orElse(null);
        Assert.assertNotNull(detail);
        // Closed, as `records.bal:137` declares it. Worth asserting HERE and not only in the corpus snapshot:
        // this record is the one a patch rewrites, and `mapRecord` rebuilt it through a constructor that
        // defaulted `isClosed` to false — silently reopening the single declaration a patch had touched.
        Assert.assertTrue(detail.isClosed());
        Assert.assertEquals(detail.fields().size(), 1);
        RecordField only = detail.fields().get(0);
        Assert.assertEquals(only.form(), RecordField.Form.INCLUSION);
        Assert.assertEquals(only.type().name(), "parser:ErrorDetail");
    }

    /**
     * HTTP-03. The service packages describe their own services, and now say so.
     *
     * <p>{@code addGenericServices} replaced whatever Central described with three comment lines, on the
     * premise that it described nothing. It described everything: http publishes one listener and seven
     * service types, graphql one and two, and every one of them is a real {@code distinct service object} in
     * its package's source. The three lines it substituted were also wrong about the listener — they gave
     * http's {@code init} one parameter of two and dropped its {@code returns ListenerError?}, and gave
     * graphql's {@code int} where the declared type is {@code int|http:Listener}.
     */
    @Test
    public void theServicePackagesDescribeTheirOwnServices() {
        Assert.assertEquals(
                FixtureCorpus.libraryFor("ballerina__http").services().stream()
                        .map(Service::name)
                        .toList(),
                List.of("Service", "ServiceContract", "RequestInterceptor", "ResponseInterceptor",
                        "RequestErrorInterceptor", "ResponseErrorInterceptor", "InterceptableService"));
        Assert.assertEquals(
                FixtureCorpus.libraryFor("ballerina__graphql").services().stream()
                        .map(Service::name)
                        .toList(),
                List.of("Service", "Interceptor"));
        // Templates a service author can copy, rather than a comment naming a listener the caller's module
        // cannot resolve.
        String document = FixtureCorpus.readSnapshot("ballerina__http");
        Assert.assertFalse(document.contains("// --- Service (generic) ---"));
        Assert.assertTrue(document.contains("service http:Service on new http:Listener(port, config) {"));
        // HTTP-14: the six service types the listener's `attach` does not name get no template, and their
        // contract is not lost — it is the declaration in the Types section, which is where it always was.
        Assert.assertFalse(document.contains("service http:InterceptableService on new"));
        Assert.assertTrue(document.contains(
                "    function createInterceptors() returns Interceptor|Interceptor[];"));
        Assert.assertTrue(FixtureCorpus.readSnapshot("ballerina__graphql").contains(
                "    isolated remote function execute(Context context, Field 'field) returns anydata|error;"));
    }

    /**
     * The one correction the corpus cannot reach, given a tripwire of its own.
     *
     * <p>There is no {@code ballerinax/client.config} fixture, so until now this was a verified fact with
     * nothing guarding it — one deleted line and nothing would have gone red. It is verified: the compiler
     * rejects {@code import ballerinax/client.config;} with {@code invalid token 'client'} and accepts
     * {@code import ballerinax/'client.config;}. The rejected token is {@code client}, a keyword that
     * prefixes {@code client class} — not {@code config}, which the patch's javadoc used to blame.
     *
     * <p>Asserted through {@code applyPatches} on a hand-built library rather than through a fixture, because
     * the fact under test is about the NAME and needs no payload at all.
     */
    @Test
    public void aReservedWordInAModulePathIsQuotedInTheImportHeader() {
        Library library = new Library("ballerinax/client.config", "", List.of(), List.of(), List.of(),
                List.of(), List.of());
        Assert.assertEquals(Patches.applyPatches(library).name(), "ballerinax/'client.config");
        // The quoting is the module path's, not every package's: an ordinary name is left alone.
        Assert.assertEquals(
                Patches.applyPatches(library.withName("ballerinax/kafka")).name(), "ballerinax/kafka");
    }

    @Test
    public void applyingThePatchesTwiceChangesNothing() {
        // Two properties in one, and both are worth keeping. For github and postgresql it says the patches are
        // narrow: nothing keys on them, so a second pass is trivially identity. For kafka — which a patch DOES
        // key on since the detail-argument table reached it — it says the correction is idempotent, because a
        // restored intersection no longer ends in the bare `error` the pattern looks for. A correction that
        // applied twice would render `error<PartitionOffset><PartitionOffset>`.
        for (String slug : new String[] {"ballerinax__github", "ballerinax__kafka", "ballerinax__postgresql"}) {
            Assert.assertEquals(
                    Patches.applyPatches(FixtureCorpus.libraryFor(slug)),
                    FixtureCorpus.libraryFor(slug),
                    slug + " must be unchanged by a second pass, because no patch keys on it");
        }
    }
}
