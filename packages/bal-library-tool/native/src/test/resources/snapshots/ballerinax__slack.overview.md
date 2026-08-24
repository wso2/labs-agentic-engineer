<!-- bal library overview v1 -->
# ballerinax/slack 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/slack` |
| Classes | none |
| Module functions | none |
| Errors | none declared here; each operation names its error type in its `returns` clause |
| Types | 506 declarations (436 records, 70 type aliases), not listed here — read one with `type` |
| Guide | 101 lines — `bal library guide ballerinax/slack` |

Guide chunks (4): 1. `Step 1: Import the module`  2. `Step 2: Instantiate a new connector`  3. `Send a Text Message to General Channel`  4. `Step 4: Run the Ballerina application` — `bal library guide ballerinax/slack <n>`

## Next

- `bal library client ballerinax/slack Client` — the client's whole callable surface
- `bal library overview ballerinax/slack -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/slack <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 174 resource · `bal library client ballerinax/slack Client`

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/slack readme usage -->

```ballerina
import ballerinax/slack;
```

```ballerina
configurable string token = ?;

slack:Client slack = check new({
    auth: {
        token
    }
});
```

```ballerina
slack:ChatPostMessageResponse postMessageResponse = check slack->/chat\.postMessage.post({channel: "general", text: "hello"});
```

<!-- guide: end ballerinax/slack readme usage -->
