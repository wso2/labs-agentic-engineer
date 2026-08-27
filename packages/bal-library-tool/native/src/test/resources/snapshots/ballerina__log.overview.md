<!-- bal library overview v1 -->
# ballerina/log 0.0.0-fixture

| | |
|---|---|
| Clients | none |
| Classes | 3 — `Logger`, `LoggerRegistry`, `PrintableRawTemplate` — `bal library class ballerina/log` |
| Module functions | 11 — `bal library funcs ballerina/log` |
| Errors | 1 — `Error` — read one with `bal library type ballerina/log <Name>` |
| Types | 23 declarations (8 records, 5 type aliases, 6 enums, 1 constants, 3 classes and object types), not listed here — read one with `type` |
| Guide | 275 lines — `bal library guide ballerina/log` |

Guide chunks (11): 1. `Log Levels`  2. `Logging API`  3. `Log Output and Format`  4. `Log Rotation`  5. `Root Context`  6. `Contextual Logging`  7. `Runtime Log Level Modification`  8. `Sensitive Data Masking`  9. `Sensitive Data Annotation`  10. `Masking Strategies`  11. `Masked String Function` — `bal library guide ballerina/log <n>`

## Next

- `bal library class ballerina/log` — 3 classes and object types, called with `.`
- `bal library funcs ballerina/log` — 11 functions callable without a client
- `bal library overview ballerina/log -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerina/log <Name> [-r]` — a declaration whole, with the types it names

## Classes and object types — 3

- `Logger` — 7 normal · `bal library class ballerina/log Logger`
- `LoggerRegistry` — 2 normal · `bal library class ballerina/log LoggerRegistry`
- `PrintableRawTemplate` — nothing callable · `bal library class ballerina/log PrintableRawTemplate`

## Module-level functions — 11, call with `.`

```
evaluateTemplate   fromConfig         getLoggerRegistry  printDebug
printError         printInfo          printWarn          processTemplate
root               setOutputFile      toMaskedString
```

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerina/log readme usage -->

```ballerina
log:printDebug("debug log");
log:printError("error log");
log:printInfo("info log");
log:printWarn("warn log");
```

```ballerina
log:printError("error log with cause", err);
log:printInfo("info log", id = 845315, name = "foo", successful = true);
```

```ballerina
log:Logger logger = check log:fromConfig(
    destinations = [
        {
            'type: log:FILE,
            path: "./logs/app.log",
            rotation: {
                policy: log:BOTH,
                maxFileSize: 10485760,  // 10MB
                maxAge: 86400,        // 24 hours
                maxBackupFiles: 7
            }
        }
    ]
);
```

```ballerina
log:Logger parentLogger = log:root();
    log:Logger childLogger = parentLogger.withContext("userId": "12345", "requestId": "abcde");
    childLogger.printInfo("User logged in");
```

```ballerina
log:Config auditLogConfig = {
            level: log:INFO,
            format: "json",
            destinations: ["./logs/audit.log"]
    };
    log:Logger auditLogger = log:fromConfig(auditLogConfig);
    auditLogger.printInfo("Hello World from the audit logger!");
```

```ballerina
// Create a logger with an explicit ID
log:Logger paymentLogger = check log:fromConfig(id = "payment-service", level = log:INFO);

// Change the level at runtime
check paymentLogger.setLevel(log:DEBUG);
log:Level current = paymentLogger.getLevel(); // DEBUG
```

```ballerina
log:LoggerRegistry registry = log:getLoggerRegistry();

// List all registered logger IDs
string[] ids = registry.getIds();
// e.g., ["root", "myorg/payment:payment-service", "myorg/payment:init"]

// Look up a logger by ID and update its level
log:Logger? logger = registry.getById("myorg/payment:payment-service");
if logger is log:Logger {
    check logger.setLevel(log:DEBUG);
}
```

```ballerina
import ballerina/log;

type User record {
    string id;
    @log:Sensitive
    string password;
    string name;
};

public function main() {
    User user = {id: "U001", password: "mypassword", name: "John Doe"};
    log:printInfo("user details", user = user);
}
```

```ballerina
import ballerina/log;

isolated function maskString(string input) returns string {
    if input.length() <= 2 {
        return "****";
    }
    return input.substring(0, 1) + "****" + input.substring(input.length() - 1);
}

type User record {
    string id;
    @log:Sensitive {
        strategy: {
            replacement: "****"
        }   
    }
    string password;
    @log:Sensitive {
        strategy: {
            replacement: maskString
        }
    }
    string ssn;
    string name;
};
```

```ballerina
User user = {id: "U001", password: "mypassword", name: "John Doe"};
string maskedUser = log:toMaskedString(user);
io:println(maskedUser); // {"id":"U001","name":"John Doe"}
```

<!-- guide: end ballerina/log readme usage -->
