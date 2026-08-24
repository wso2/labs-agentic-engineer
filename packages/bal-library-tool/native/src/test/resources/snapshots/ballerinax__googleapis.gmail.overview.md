<!-- bal library overview v1 -->
# ballerinax/googleapis.gmail 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/googleapis.gmail` |
| Classes | none |
| Module functions | none |
| Errors | 3 — `Error`, `FileGenericError`, `ValueEncodeError` — read one with `bal library type ballerinax/googleapis.gmail <Name>` |
| Types | 35 declarations (31 records, 2 type aliases, 2 constants), not listed here — read one with `type` |
| Guide | 153 lines — `bal library guide ballerinax/googleapis.gmail` |

Guide chunks (5): 1. `Step 1: Import the module`  2. `Step 2: Instantiate a new connector`  3. `Get unread emails in INBOX`  4. `Send email`  5. `Step 4: Run the Ballerina application` — `bal library guide ballerinax/googleapis.gmail <n>`

## Next

- `bal library client ballerinax/googleapis.gmail Client` — the client's whole callable surface
- `bal library overview ballerinax/googleapis.gmail -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/googleapis.gmail <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 32 resource · `bal library client ballerinax/googleapis.gmail Client`

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/googleapis.gmail readme usage -->

```ballerina
import ballerinax/googleapis.gmail;
```

```ballerina
configurable string refreshToken = ?;
configurable string clientId = ?;
configurable string clientSecret = ?;

gmail:Client gmail = check new gmail:Client(
    config = {
        auth: {
            refreshToken,
            clientId,
            clientSecret
        }
    }
);
```

```ballerina
gmail:MessageListPage messageList = check gmail->/users/me/messages(q = "label:INBOX is:unread");
```

```ballerina
gmail:MessageRequest message = {
    to: ["<recipient>"],
    subject: "Scheduled Maintenance Break Notification",
    bodyInHtml: string `<html>
                            <head>
                                <title>Scheduled Maintenance</title>
                            </head>
                        </html>`;
};

gmail:Message sendResult = check gmail->/users/me/messages/send.post(message);
```

<!-- guide: end ballerinax/googleapis.gmail readme usage -->
