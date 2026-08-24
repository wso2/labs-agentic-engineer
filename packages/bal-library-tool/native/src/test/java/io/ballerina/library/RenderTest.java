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

import io.ballerina.library.model.ClientClass;
import io.ballerina.library.model.Fn;
import io.ballerina.library.model.Library;
import io.ballerina.library.model.ModuleRef;
import io.ballerina.library.model.Param;
import io.ballerina.library.model.RecordField;
import io.ballerina.library.model.ReturnDef;
import io.ballerina.library.model.Service;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.model.TypeRef;
import io.ballerina.library.render.Documents;
import io.ballerina.library.render.Report;
import io.ballerina.library.render.Signatures;
import io.ballerina.library.render.TypeDefs;
import org.testng.Assert;
import org.testng.annotations.Test;

import java.util.List;
import java.util.Optional;

/**
 * Rendering rules, one at a time.
 *
 * <p>The corpus proves the whole pipeline against real packages; these pin the individual decisions, so a
 * snapshot diff has something to be explained by.
 *
 * @since 0.1.0
 */
public class RenderTest {

    private static final Library EMPTY = new Library(
            "test/pkg", "", List.of(), List.of(), List.of(), List.of(), List.of());

    private static final TypeDef.Rec RECORD = new TypeDef.Rec("Stars", "A star count.", List.of(
            new RecordField("owner", "", new TypeRef("string")),
            new RecordField("count", "", new TypeRef("int"), "0", true)));

    /**
     * Line one states the document's own length, in both registers.
     *
     * <p>The only defence the tool has against a filter. Piping is not a habit this tool can argue an agent out
     * of — measured at 19 of 19 calls before the skill said "never pipe it" and 19 of 19 after — and a
     * {@code | head -150} is silent: the closure of one github operation is 535 lines, so 72% of it went missing
     * with nothing in what came back to say so. One of those cuts landed mid-record, and the model was handed a
     * record whose last field had no closing brace.
     *
     * <p>A length on line one turns that silence into arithmetic the reader can do: 150 lines arrived, the
     * document says 535. It survives every filter that could cause the problem, because line one is the one line
     * a window always keeps.
     */
    @Test
    public void lineOneStatesTheDocumentsOwnLength() {
        String report = Documents.withLength("<!-- bal library client v1 -->\n# Clients\n\nbody\n");
        Assert.assertEquals(report.lines().findFirst().orElseThrow(),
                "<!-- bal library client v1 · 4 lines -->");

        String code = Documents.withLength("// ballerinax/github:6.0.0\n\npublic type X record {|\n|};\n");
        Assert.assertEquals(code.lines().findFirst().orElseThrow(),
                "// ballerinax/github:6.0.0 · 4 lines");

        // The count is the WHOLE document, so a reader comparing it against what arrived is comparing like with
        // like. Counting the body only would make a complete 4-line document look like a cut 3-line one.
        Assert.assertEquals(code.lines().count(), 4);
        Assert.assertEquals(report.lines().count(), 4);

        // A one-line document is still stamped: `funcs` on a package with none is exactly that, and "1 lines"
        // reading oddly is a smaller cost than a reader unable to tell a complete answer from a cut one.
        Assert.assertEquals(Documents.withLength("// nothing\n"), "// nothing · 1 lines\n");

        // Anything whose first line is neither marker is left alone rather than guessed at. `--help` goes out
        // through a different path and has no business carrying a length.
        String plain = "Usage: bal library find <keywords>...\n";
        Assert.assertEquals(Documents.withLength(plain), plain);
    }

    @Test
    public void aModuleAliasIsTheLastDottedSegmentOfThePackagePath() {
        Assert.assertEquals(new ModuleRef("ballerinax", "googleapis.gmail").prefix(), "gmail");
        Assert.assertEquals(new ModuleRef("ballerina", "http").prefix(), "http");
    }

    @Test
    public void aForeignNameIsQualifiedOnceNeverTwice() {
        List<Signatures.ExternalLink> links = List.of(
                new Signatures.ExternalLink("Message", new ModuleRef("ballerinax", "googleapis.gmail")));
        Assert.assertEquals(Signatures.applyPrefixToTypeName("Message", links), "gmail:Message");
        Assert.assertEquals(Signatures.applyPrefixToTypeName("gmail:Message", links), "gmail:Message");
        // Inside a union and an array, both members still get qualified.
        Assert.assertEquals(
                Signatures.applyPrefixToTypeName("Message[]|error", links), "gmail:Message[]|error");
        // A longer name that merely contains it is a different type.
        Assert.assertEquals(Signatures.applyPrefixToTypeName("MessageList", links), "MessageList");
    }

    @Test
    public void theAgentNoteGroupsNamesByThePackageTheyComeFrom() {
        String note = Signatures.buildSpecialAgentNote(List.of(
                new Signatures.ExternalLink("Message", new ModuleRef("ballerinax", "googleapis.gmail")),
                new Signatures.ExternalLink("Error", new ModuleRef("ballerina", "sql")),
                new Signatures.ExternalLink("Draft", new ModuleRef("ballerinax", "googleapis.gmail"))));
        Assert.assertEquals(note,
                " // Special Agent Note: Message, Draft FROM ballerinax/googleapis.gmail module, "
                        + "Error FROM ballerina/sql module");
        Assert.assertEquals(Signatures.buildSpecialAgentNote(List.of()), "");
    }

    @Test
    public void theAgentNoteNamesAForeignTypeOncePerTypeNotOncePerMention() {
        // EMAIL-04. `mime:Entity|Attachment|(mime:Entity|Attachment)[]` mentions one foreign name twice — once
        // bare and once as the array's element — and the note read `Entity, Entity FROM ballerina/mime module`,
        // which is either a mangle or a claim that the line needs two imports.
        Signatures.ExternalLink entity =
                new Signatures.ExternalLink("Entity", new ModuleRef("ballerina", "mime"));
        Assert.assertEquals(
                Signatures.buildSpecialAgentNote(List.of(entity, entity)),
                " // Special Agent Note: Entity FROM ballerina/mime module");
        // Keyed on the name AND the package, so two packages that both declare `Error` still name both.
        Assert.assertEquals(
                Signatures.buildSpecialAgentNote(List.of(
                        new Signatures.ExternalLink("Error", new ModuleRef("ballerina", "sql")),
                        new Signatures.ExternalLink("Error", new ModuleRef("ballerinax", "kafka")))),
                " // Special Agent Note: Error FROM ballerina/sql module, "
                        + "Error FROM ballerinax/kafka module");
        // The record-field path is the one that had no dedup, so it gets the assertion too: this is the exact
        // shape of `email:Message.attachments`.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Rec("Message", "", List.of(
                        new RecordField("attachments", "", new TypeRef(
                                "Entity|Attachment|(Entity|Attachment)[]",
                                List.of(new TypeRef.Link.External(new ModuleRef("ballerina", "mime"), "Entity"),
                                        new TypeRef.Link.External(new ModuleRef("ballerina", "mime"), "Entity"))),
                                null, true)))),
                "public type Message record {\n"
                        + "    mime:Entity|Attachment|(mime:Entity|Attachment)[] attachments?;"
                        + " // Special Agent Note: Entity FROM ballerina/mime module\n};");
    }

    @Test
    public void aDeclarationIsPublicAndAnObjectFieldIsTooButARecordFieldCannotBe() {
        // SLACK-11. Central publishes no declaration-level visibility at all, so this comes from what it
        // publishes AT ALL: 2,085 public type declarations, 229 classes, 178 constants, 13 enums and 9 module
        // functions across the corpus, with all 708 module-private declarations withheld and no exception.
        // `public` on a RECORD field is `invalid token 'public'`; on a class field it is required, because an
        // including class must repeat the visibility of the field it overrides — the 82 `mismatched visibility
        // qualifiers` postgresql's value classes produced from `*sql:TypedValue`.
        RecordField field = new RecordField("value", "", new TypeRef("string"));
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Rec("Row", "", List.of(field))),
                "public type Row record {\n    string value;\n};");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ObjectDef(
                        "BitStringValue", "", TypeDef.ObjectDef.Form.CLASS,
                        TypeDef.ObjectDef.Role.PLAIN, false, false, false, false,
                        List.of(RecordField.inclusion(new TypeRef("TypedValue")), field),
                        List.of())),
                // The inclusion takes neither qualifier: it says where the members come from, it is not one.
                "public class BitStringValue {\n    *TypedValue;\n    public string value;\n}");
    }

    @Test
    public void isolatedIsRenderedOnEveryCallableFormAndOnTheClientClass() {
        // SLACK-11's other half: 1,615 of the corpus's 1,617 methods carry `isIsolated` and none of them said
        // so. The qualifier sits between visibility and `remote`/`resource`, which is where the language puts
        // it — verified by compiling each form.
        Assert.assertEquals(
                Signatures.renderSignature(new Fn.Remote(
                        "send", "", List.of(), ReturnDef.none(), false, true)),
                "isolated remote function send();");
        Assert.assertEquals(
                Signatures.renderSignature(new Fn.Resource(
                        "get", List.of(new Fn.PathSegment.Literal("apps")), "", List.of(),
                        ReturnDef.none(), false, true)),
                "isolated resource function get apps();");
        Assert.assertEquals(
                Signatures.renderSignature(new Fn.Constructor("", List.of(), ReturnDef.none(), false, true)),
                "isolated function init();");
        // A module-level function is the one callable that carries visibility of its own.
        Assert.assertEquals(
                Signatures.renderStandaloneFunction(new Fn.Normal(
                        "getDefaultListener", "", List.of(), new ReturnDef(new TypeRef("Listener")),
                        false, true)),
                "public isolated function getDefaultListener() returns Listener;");
        // And the client class, which is the first thing an isolated caller has to construct.
        Assert.assertTrue(Documents.toSyntaxString(EMPTY.withClients(List.of(
                        new ClientClass("Client", "", true, List.of()))))
                .contains("public isolated client class Client {"), "the client class keeps its qualifiers");
        Assert.assertTrue(Documents.toSyntaxString(EMPTY.withClients(List.of(
                        new ClientClass("Client", "", false, List.of()))))
                .contains("public client class Client {"), "and does not gain one it does not have");
    }

    @Test
    public void theApiDocumentDocumentsAParameterAndACompactViewQuotesTheSignature() {
        // SLACK-12, and the reason it is not wired into the quoted form: `overview` and `ops` answer inside a
        // byte budget. The DECLARATION is identical either way, which is what keeps the views honest.
        Fn.Remote send = new Fn.Remote(
                "send",
                "Post a message.",
                List.of(new Param("channel", "The channel ID, not its name.", new TypeRef("string"))),
                new ReturnDef(new TypeRef("error?"), "nil on success"),
                false,
                false);
        Assert.assertEquals(
                Signatures.renderSignature(send),
                "# Post a message.\nremote function send(string channel) returns error?;");
        Assert.assertTrue(Documents.toSyntaxString(EMPTY.withClients(List.of(
                        new ClientClass("Client", "", false, List.of(send)))))
                .contains("    # Post a message.\n"
                        + "    # + channel - The channel ID, not its name.\n"
                        + "    # + return - nil on success\n"
                        + "    remote function send(string channel) returns error?;"));
    }

    @Test
    public void anAnnotationCarriesItsConfigRecordAndEveryAttachmentPoint() {
        // HTTP-07. `service_function` was a label invented for a two-value enum, and `invalid token
        // 'service_function'` is what the compiler says about it; Central's own string is the source's `on`
        // clause for all twelve annotations in the corpus.
        String rendered = Documents.toSyntaxString(new Library(
                "test/pkg", "", List.of(), List.of(), List.of(), List.of(),
                List.of(new Library.AnnotationDef(
                                "Payload", "Defines the payload.",
                                Optional.of(new TypeRef("HttpPayload")), "parameter, return"),
                        new Library.AnnotationDef(
                                "ResourceConfig", "", Optional.of(new TypeRef("HttpResourceConfig")),
                                "object function"),
                        // A marker annotation takes no argument, so printing a type here would be a guess.
                        new Library.AnnotationDef("ID", "", Optional.empty(),
                                "record field, parameter, return"))));
        Assert.assertTrue(rendered.contains(
                "# Defines the payload.\npublic annotation HttpPayload Payload on parameter, return;"),
                rendered);
        Assert.assertTrue(rendered.contains(
                "public annotation HttpResourceConfig ResourceConfig on object function;"), rendered);
        Assert.assertTrue(rendered.contains(
                "public annotation ID on record field, parameter, return;"), rendered);
        Assert.assertFalse(rendered.contains("service_function"), "a token the compiler rejects");
    }

    @Test
    public void anEnumMemberKeepsTheDescriptionCentralPublishesForIt() {
        // PSQL-04's recoverable half. The VALUE is not recoverable — `pgoutput` appears zero times in
        // postgresql's payload — so the description is the whole of what distinguishes one member from another.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Enumeration("Plugin", "The plugins.", List.of(
                        new TypeDef.Enumeration.Member("PGOUTPUT", "The standard plug-in."),
                        new TypeDef.Enumeration.Member("DECODERBUFS", "")))),
                "# The plugins.\npublic enum Plugin {\n    # The standard plug-in.\n    PGOUTPUT,\n"
                        + "    DECODERBUFS\n}");
    }

    @Test
    public void everyTypeKindRenders() {
        Assert.assertEquals(TypeDefs.renderTypeDef(RECORD),
                "# A star count.\npublic type Stars record {\n    string owner;\n    int count? = 0;\n};");
        // Spacing is uniform across kinds: a doc comment is adjacent to what it documents, and a declaration
        // with no description starts at its first line. Records were the one form that did neither (IO-03).
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Rec(RECORD.name(), "", RECORD.fields())),
                "public type Stars record {\n    string owner;\n    int count? = 0;\n};");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Enumeration("Colour", "", List.of(
                        new TypeDef.Enumeration.Member("RED", ""),
                        new TypeDef.Enumeration.Member("GREEN", "")))),
                "public enum Colour {\n    RED,\n    GREEN\n}");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Alias("Id", "", new TypeRef("int|string"))),
                "public type Id int|string;");
        // One Alias case for all seventeen of Central's alias categories, so the rendering must not care
        // which one a declaration came from.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Alias("TsDef", "", new TypeRef("string"))),
                "public type TsDef string;");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Alias("Pair", "", new TypeRef("[string, int]"))),
                "public type Pair [string, int];");
        // An alias whose descriptor could not be encoded still names itself, as a comment — and a comment takes
        // no visibility qualifier, because it is not a declaration.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Alias("Opaque", "", new TypeRef(""))),
                "// Unknown type: Opaque");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Constant("NAME", "", "aep", new TypeRef("string"))),
                "public const string NAME = \"aep\";");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ObjectDef("Engine", "")), "public class Engine {\n}");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef(
                        "ClientError", "", true, Optional.of(new TypeRef("Error")))),
                "public type ClientError distinct Error;");
    }

    @Test
    public void aStringConstantIsQuotedExactlyOnce() {
        // Central sends a string constant's value WITH its quotes — `"\"PLAIN\""` for AUTH_SASL_PLAIN — and
        // adding another pair produced `const string X = ""PLAIN"";`, which is not valid Ballerina. It affected
        // all 108 string constants in the corpus, with zero counter-examples.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(
                        new TypeDef.Constant("AUTH", "", "\"PLAIN\"", new TypeRef("string"))),
                "public const string AUTH = \"PLAIN\";");
        // An unquoted value still gains quotes, because "already quoted" is an observation about today's payload
        // and a bare identifier would look like a reference rather than like a typo.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Constant("AUTH", "", "PLAIN", new TypeRef("string"))),
                "public const string AUTH = \"PLAIN\";");
        // A non-string constant is never quoted.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Constant("PORT", "", "9092", new TypeRef("int"))),
                "public const int PORT = 9092;");
    }

    @Test
    public void anErrorRendersAsTheFourCombinationsOfTheTwoFactsCentralPublishes() {
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef(
                        "SslError", "", true, Optional.of(new TypeRef("ClientError")))),
                "public type SslError distinct ClientError;");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef("Error", "", true, Optional.empty())),
                "public type Error distinct error;");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef(
                        "X", "", false, Optional.of(new TypeRef("A|B|C")))),
                "public type X A|B|C;");
        // The absent-base default is `error` rather than nothing, because an error at the top of its own
        // hierarchy narrows the language's `error`, and that is what its declaration says.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef("X", "", false, Optional.empty())),
                "public type X error;");
    }

    @Test
    public void aDetailRecordBaseIsWrappedInErrorRatherThanPrintedAsASupertype() {
        // Central publishes one key, `detailType`, for two different things. Printed bare, a detail
        // record produced `public type FHIRServerError distinct FHIRServerErrorDetails;` — a declaration
        // naming a RECORD where an error type has to be. It does not compile, it cannot be `is`-tested,
        // and it contradicts the tool's own promise that a signature from here is the source for what
        // compiles. Published source: `distinct error<FHIRServerErrorDetails>`.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef(
                        "FHIRServerError", "", true, Optional.of(new TypeRef("FHIRServerErrorDetails")), true)),
                "public type FHIRServerError distinct error<FHIRServerErrorDetails>;");
        // The supertype reading is unchanged, and is what every fixture in the corpus publishes.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef(
                        "SslError", "", true, Optional.of(new TypeRef("ClientError")), false)),
                "public type SslError distinct ClientError;");
        // A detail record with no base cannot arise, but must not render `error<error>` if it does.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ErrorDef("X", "", true, Optional.empty(), true)),
                "public type X distinct error;");
    }

    @Test
    public void aDescriptionBecomesABallerinaDocComment() {
        Assert.assertTrue(TypeDefs.renderTypeDef(RECORD).startsWith("# A star count.\n"));
    }

    @Test
    public void pathParametersAreDeclaredInThePathAndNotRepeatedInTheParameterList() {
        String rendered = Documents.toSyntaxString(EMPTY.withClients(List.of(
                new ClientClass("Client", "", List.of(new Fn.Resource(
                        "get",
                        List.of(new Fn.PathSegment.Literal("repos"),
                                new Fn.PathSegment.Parameter("string", "owner")),
                        "",
                        List.of(new Param("owner", "", new TypeRef("string")),
                                new Param("page", "", new TypeRef("int"), "1")),
                        new ReturnDef(new TypeRef("json"))))))));
        Assert.assertTrue(rendered.contains(
                "resource function get repos/[string owner](int page = 1) returns json;"), rendered);
    }

    @Test
    public void aServiceTemplateNamesTheListenerAndTheContractItImplies() {
        String rendered = Documents.toSyntaxString(EMPTY.withServices(List.of(new Service(
                "ConsumerService",
                false,
                new Service.Listener("kafka:Listener", List.of(
                        new Param("config", "", new TypeRef("ConsumerConfiguration")))),
                List.of(new Fn.Remote(
                        "onConsumerRecord",
                        "",
                        List.of(new Param("records", "", new TypeRef("BytesConsumerRecord[]"))),
                        new ReturnDef(new TypeRef("error?"))))))));
        // ARGUMENTS, not a parameter declaration list: `new kafka:Listener(ConsumerConfiguration config)` was
        // six compiler errors on one line, of which the first was "too many arguments in call to 'new()'".
        Assert.assertTrue(rendered.contains(
                "service kafka:ConsumerService on new kafka:Listener(config) {"), rendered);
        Assert.assertTrue(rendered.contains(
                "remote function onConsumerRecord(BytesConsumerRecord[] records) returns error?;"), rendered);
    }

    @Test
    public void aServiceMethodIsRenderedByTheSameCodeAsEveryOtherCallable() {
        // KAFKA-10: the hand-rolled service-method renderer dropped defaults and optionality, which every
        // other callable in the document keeps. Sharing one renderer is what makes that structural.
        String rendered = Documents.toSyntaxString(EMPTY.withServices(List.of(new Service(
                "Service",
                false,
                new Service.Listener("pkg:Listener", List.of()),
                List.of(new Fn.Remote(
                        "onMessage",
                        "Called per message.",
                        List.of(new Param("retries", "", new TypeRef("int"), "3"),
                                new Param("tag", "", new TypeRef("string?"))),
                        new ReturnDef(new TypeRef("error?"))))))));
        Assert.assertTrue(rendered.contains("""
                service pkg:Service on new pkg:Listener() {
                    # Called per message.
                    remote function onMessage(int retries = 3, string? tag) returns error?;
                }"""), rendered);
    }

    @Test
    public void anObjectDeclarationSaysWhetherItCanBeConstructedAndHowItIsCalled() {
        // SQL-01/SQL-02. `class` is instantiable and an `object` type is a contract, so printing the one as
        // the other tells a caller to `new` an abstract type; `client` is why the call is `->` and not `.`.
        TypeDef.ObjectDef contract = new TypeDef.ObjectDef(
                "Client", "A database client.",
                TypeDef.ObjectDef.Form.OBJECT_TYPE, TypeDef.ObjectDef.Role.CLIENT,
                false, false, true, false,
                List.of(),
                List.of(new Fn.Remote(
                        "query", "",
                        List.of(new Param("sqlQuery", "", new TypeRef("ParameterizedQuery"))),
                        new ReturnDef(new TypeRef("stream<rowType, Error?>")))));
        Assert.assertEquals(TypeDefs.renderTypeDef(contract), """
                # A database client.
                public type Client isolated client object {
                    remote function query(ParameterizedQuery sqlQuery) returns stream<rowType, Error?>;
                };""");

        // A service type is a `service object` even though Central's `isService` is false on every one of
        // them, and `readonly`/`distinct` are the two qualifiers that change what a value of it may do.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ObjectDef(
                        "Service", "", TypeDef.ObjectDef.Form.OBJECT_TYPE,
                        TypeDef.ObjectDef.Role.SERVICE, true, false, false, false,
                        List.of(), List.of())),
                "public type Service distinct service object {\n};");
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.ObjectDef(
                        "StatusAccepted", "", TypeDef.ObjectDef.Form.CLASS,
                        TypeDef.ObjectDef.Role.PLAIN, false, true, false, false,
                        List.of(RecordField.inclusion(new TypeRef("Status"))), List.of())),
                "public readonly class StatusAccepted {\n    *Status;\n}");
    }

    @Test
    public void aCallableWithNoReturnHasNoReturnsClause() {
        // HTTP-05: `nil` is the ENGLISH name of the basic type — Ballerina spells it `()` — so twelve of
        // http's declarations named a type the compiler does not have.
        Assert.assertEquals(
                Signatures.renderSignature(new Fn.Normal(
                        "circuitBreakerForceClose", "", List.of(), ReturnDef.none())),
                "function circuitBreakerForceClose();");
    }

    @Test
    public void aDefaultTheDocumentCannotNameSaysSoOnTheLine() {
        // SHEETS-02. The expression stays: a defaultable parameter printed without its default reads as
        // required, which is a worse claim than an unwritable default.
        Assert.assertEquals(
                Signatures.renderSignature(new Fn.Normal(
                        "init", "",
                        List.of(new Param("serviceUrl", "", new TypeRef("string"), "BASE_URL")
                                .withUnwritableDefault()),
                        ReturnDef.none())),
                "function init(string serviceUrl = BASE_URL); // Special Agent Note: the default BASE_URL is "
                        + "not exported by this package; omit the argument rather than repeating it");
        // Two of them, and a foreign type name as well, all in ONE trailing comment — a second `//` would
        // sit inside the first.
        String rendered = Signatures.renderSignature(new Fn.Normal(
                "init", "",
                List.of(new Param("config", "", new TypeRef("Config", List.of(
                                new TypeRef.Link.External(new ModuleRef("ballerina", "http"), "Config")))),
                        new Param("a", "", new TypeRef("string"), "BASE_URL").withUnwritableDefault(),
                        new Param("b", "", new TypeRef("string"), "DRIVE_BASE_URL").withUnwritableDefault()),
                ReturnDef.none()));
        Assert.assertEquals(rendered,
                "function init(http:Config config, string a = BASE_URL, string b = DRIVE_BASE_URL); "
                        + "// Special Agent Note: Config FROM ballerina/http module, the defaults BASE_URL, "
                        + "DRIVE_BASE_URL are not exported by this package; omit the arguments rather than "
                        + "repeating them");
    }

    @Test
    public void aListenerIsADeclarationInItsOwnSection() {
        // PSQL-03/KAFKA-04/EMAIL-01: the listener is the entry point to a package's service half, and it
        // printed nowhere at all — `type ballerinax/postgresql CdcListener` failed.
        String rendered = Documents.toSyntaxString(new Library(
                "test/pkg", "", List.of(), List.of(), List.of(),
                List.of(new TypeDef.ObjectDef(
                        "CdcListener", "The CDC listener.",
                        TypeDef.ObjectDef.Form.CLASS, TypeDef.ObjectDef.Role.PLAIN,
                        false, false, true, false,
                        List.of(),
                        List.of(new Fn.Constructor(
                                "",
                                List.of(new Param("config", "", new TypeRef("Config"), null, Param.Form.INCLUSION)),
                                ReturnDef.none())))),
                List.of(), List.of(), List.of()));
        Assert.assertTrue(rendered.contains("""
                // --- Listeners ---

                # The CDC listener.
                public isolated class CdcListener {
                    function init(*Config config);
                }"""), rendered);
    }

    @Test
    public void aSectionWithNothingInItPrintsNoBanner() {
        String rendered = Documents.toSyntaxString(EMPTY);
        Assert.assertFalse(rendered.contains("// --- Types ---"));
        Assert.assertFalse(rendered.contains("// --- Client ---"));
        Assert.assertEquals(rendered,
                "// ============================================================\n"
                        + "// Library: test/pkg\n"
                        + "// ============================================================\n"
                        + "import test/pkg;\n");
    }

    @Test
    public void columnsAreWideEnoughForTheLongestEntryAndNeverTruncate() {
        // Truncating a name would make it unusable as the next command's argument.
        // Column width is the longest entry plus one, and one more space joins them, so every column starts on a
        // 9-character boundary here. Four per row, and the row is right-trimmed.
        List<String> rows = Report.columns(List.of("a 1", "bbbbb 2", "c 3", "d 4", "e 5"));
        Assert.assertEquals(rows, List.of("a 1      bbbbb 2  c 3      d 4", "e 5"));
    }

    @Test
    public void aReportSeparatesMultiLineDeclarationsWithABlankLine() {
        // A doc comment makes a declaration multi-line, and `# Get a repository` followed straight by the next
        // declaration's comment reads as one four-line comment on one function.
        Assert.assertTrue(new Report("client").ballerina(List.of("# doc\nfunction a();", "function b();"))
                .toString().contains("function a();\n\nfunction b();"));
        Assert.assertTrue(new Report("client").ballerina(List.of("function a();", "function b();"))
                .toString().contains("function a();\nfunction b();"));
    }

    @Test
    public void aReportOpensOnItsMarkerAndEndsWithOneNewline() {
        String document = new Report("overview").heading(1, "Title").paragraph("body").toString();
        Assert.assertEquals(document, "<!-- bal library overview v1 -->\n# Title\n\nbody\n");
    }

    @Test
    public void aModuleLevelVariableIsADeclarationAndCarriesItsInitialiser() {
        // The open half of SLACK-02/SLACK-13. Central publishes 64 module-level `public final` variables across
        // the corpus under `variables`; they were parsed and rendered nowhere, so `http:CONTINUE` — which
        // compiles from another module, measured — appeared in no verb.
        String rendered = TypeDefs.renderTypeDef(new TypeDef.Variable(
                "CONTINUE",
                "The common status code response constant of `Continue`.",
                new TypeRef("readonly & Continue"),
                "{}",
                true));
        // The initialiser is not decoration: `public final T X;` is `uninitialized variable 'X'`, and all 64 of
        // these are written `= {}` in their published sources, so this line is the source's own.
        Assert.assertEquals(rendered,
                "# The common status code response constant of `Continue`.\n"
                        + "public final readonly & Continue CONTINUE = {};");
        // A foreign type in the position still gets its prefix and its import note.
        Assert.assertEquals(
                TypeDefs.renderTypeDef(new TypeDef.Variable("DEFAULT", "", new TypeRef("Config",
                        List.of(new TypeRef.Link.External(new ModuleRef("ballerina", "http"), "Config"))),
                        "{}", false)),
                "public final http:Config DEFAULT = {};"
                        + " // Special Agent Note: Config FROM ballerina/http module");
    }

    @Test
    public void aConfigurableIsNamedInTheCodeRegisterButNeverAsADeclaration() {
        // Measured, not assumed: `http:maxActiveConnections` from another module is `attempt to refer to
        // non-accessible symbol`, because a `configurable` is module-private. So it must never be printed as
        // source and must never pick up the blanket `public` the other declarations carry.
        //
        // It is NAMED here all the same, as comments. `overview` used to carry the fact and stopped
        // (ADR-0017); `type` cannot reach it, because a configurable is not a declaration to resolve. With no
        // section here the cut would have deleted the fact from every verb rather than moved it — expensive to
        // reach was the intended cost, unreachable was not.
        Library library = new Library(
                "test/pkg", "", List.of(), List.of(), List.of(), List.of(), List.of(), List.of(),
                List.of(new Library.Configurable("maxActive", "Max connections.", new TypeRef("int"), "-1")));
        String document = Documents.toSyntaxString(library);
        Assert.assertTrue(document.contains("// maxActive = -1    # int — Max connections."), document);
        Assert.assertTrue(document.contains("[test.pkg]"), document);
        // Every line of it is a comment, so nothing here reads as something to copy.
        for (String line : document.split("\n", -1)) {
            if (line.contains("maxActive") || line.toLowerCase(java.util.Locale.ROOT).contains("config.toml")) {
                Assert.assertTrue(line.startsWith("//"), "not a comment: " + line);
            }
        }
        Assert.assertFalse(document.contains("configurable "), document);
        Assert.assertFalse(document.contains("public "), document);
    }
}
