<!-- bal library overview v1 -->
# ballerinax/googleapis.sheets 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/googleapis.sheets` |
| Classes | none |
| Module functions | none |
| Errors | 3 — `Error`, `InvalidRangeError`, `SpreadsheetError` — read one with `bal library type ballerinax/googleapis.sheets <Name>` |
| Types | 28 declarations (20 records, 1 type aliases, 6 enums, 1 constants), not listed here — read one with `type` |
| Guide | 157 lines — `bal library guide ballerinax/googleapis.sheets` |

Guide chunks (4): 1. `Step 1: Import connector`  2. `Step 2: Create a new connector instance`  3. `Create a spreadsheet with a given name`  4. `Step 4: Run the Ballerina application` — `bal library guide ballerinax/googleapis.sheets <n>`

## Next

- `bal library client ballerinax/googleapis.sheets Client` — the client's whole callable surface
- `bal library overview ballerinax/googleapis.sheets -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/googleapis.sheets <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 43 remote · `bal library client ballerinax/googleapis.sheets Client`

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/googleapis.sheets readme usage -->

```ballerina
import ballerinax/googleapis.sheets;
```

```ballerina
configurable string clientId = ?;
configurable string clientSecret = ?;
configurable string refreshToken = ?;
configurable string refreshUrl = ?;

sheets:Client spreadsheetClient = check new ({
    auth: {
        clientId,
        clientSecret,
        refreshToken,
        refreshUrl
    }
});
```

```ballerina
public function main() returns error? {

    // create a spreadsheet
    sheets:Spreadsheet response = check spreadsheetClient->createSpreadsheet("NewSpreadsheet");

    // Add a new worksheet with given name to the Spreadsheet
    string spreadsheetId = response.spreadsheetId;
    sheets:Sheet sheet = check spreadsheetClient->addSheet(spreadsheetId, "NewWorksheet");
}
```

<!-- guide: end ballerinax/googleapis.sheets readme usage -->
