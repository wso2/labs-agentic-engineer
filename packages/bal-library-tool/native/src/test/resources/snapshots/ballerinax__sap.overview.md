<!-- bal library overview v1 -->
# ballerinax/sap 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/sap` |
| Classes | none |
| Module functions | none |
| Errors | none declared here; each operation names its error type in its `returns` clause |
| Types | 3 declarations (3 type aliases), not listed here — read one with `type` |
| Guide | 125 lines — `bal library guide ballerinax/sap` |

## Quickstart

*Quoted from the package's own readme and checked against this version's declarations. A line marked `⚠` names something this version does not declare. The signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/sap readme usage -->

```ballerina
configurable string hostname = ?;
configurable string username = ?;
configurable string password = ?;

sap:Client sapClient = check new (string `https://${hostname}/sap/opu/odata/sap/API_SALES_ORDER_SRV`, {
    auth: {
        username,
        password
    }
});
```

```ballerina
json salesOrderList = check sapClient->/A_SalesOrder();
```

<!-- guide: end ballerinax/sap readme usage -->

Guide chunks (4): 1. `Step 1: Import the module`  2. `Step 2: Instantiate a new connector`  3. `Step 3: Invoke the connector operation`  4. `Step 4: Run the Ballerina application` — `bal library guide ballerinax/sap <n>`

## Next

- `bal library client ballerinax/sap Client` — the client's whole callable surface
- `bal library overview ballerinax/sap -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/sap <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 7 resource, 7 remote · `bal library client ballerinax/sap Client`
