<!-- bal library overview v1 -->
# ballerinax/twilio 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/twilio` |
| Classes | none |
| Module functions | none |
| Errors | none declared here; each operation names its error type in its `returns` clause |
| Types | 278 declarations (212 records, 66 type aliases), not listed here — read one with `type` |
| Guide | 154 lines — `bal library guide ballerinax/twilio` |

Guide chunks (4): 1. `Step 1 - Import the module`  2. `Step 2 - Create a new connector instance`  3. `Step 3 - Invoke the connector operation`  4. `Step 4: Run the Ballerina application` — `bal library guide ballerinax/twilio <n>`

## Next

- `bal library client ballerinax/twilio Client` — the client's whole callable surface
- `bal library overview ballerinax/twilio -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/twilio <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 199 remote · `bal library client ballerinax/twilio Client`

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/twilio readme usage -->

```ballerina
import ballerinax/twilio;
```

```ballerina
configurable string apiKey = ?;
configurable string apiSecret = ?;
configurable string accountSid = ?;

twilio:ConnectionConfig twilioConfig = {
    auth: {
        apiKey,
        apiSecret,
        accountSid
    }
};

twilio:Client twilio = check new (twilioConfig);
```

```ballerina
public function main() returns error? {
    twilio:CreateMessageRequest messageRequest = {
        To: "+XXXXXXXXXXX", // Phone number that you want to send the message to
        From: "+XXXXXXXXXXX", // Twilio phone number
        Body: "Hello from Ballerina"
    };

    twilio:Message response = check twilio->createMessage(messageRequest);

    // Print the status of the message from the response
    io:println("Message Status: ", response?.status);
}
```

<!-- guide: end ballerinax/twilio readme usage -->
