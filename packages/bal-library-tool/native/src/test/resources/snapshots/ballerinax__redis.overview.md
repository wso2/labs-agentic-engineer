<!-- bal library overview v1 -->
# ballerinax/redis 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/redis` |
| Classes | none |
| Module functions | none |
| Errors | 1 — `Error` — read one with `bal library type ballerinax/redis <Name>` |
| Types | 8 declarations (6 records, 1 type aliases, 1 enums), not listed here — read one with `type` |
| Guide | 114 lines — `bal library guide ballerinax/redis` |

Guide chunks (7): 1. `Step 2: Start Redis server`  2. `Step 3: Verify Redis connectivity`  3. `Step 1: Import the module`  4. `Step 2: Instantiate a new connector`  5. `Set a key-value pair`  6. `Get value by key`  7. `Step 4: Run the Ballerina application` — `bal library guide ballerinax/redis <n>`

## Next

- `bal library client ballerinax/redis Client` — the client's whole callable surface
- `bal library overview ballerinax/redis -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/redis <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 111 remote, 1 normal · `bal library client ballerinax/redis Client`

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/redis readme usage -->

```ballerina
import ballerinax/redis;
```

```ballerina
redis:Client redis = check new (
    connection = {
        host: "localhost",
        port: 6379
    }
);
```

```ballerina
check redis->set("key", "value");
```

```ballerina
string value = check redis->get("key");
```

<!-- guide: end ballerinax/redis readme usage -->
