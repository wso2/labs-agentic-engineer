<!-- bal library overview v1 -->
# ballerina/graphql 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerina/graphql` |
| Classes | 5 — `Context`, `Field`, `Service`, `Interceptor`, `Listener` — `bal library class ballerina/graphql` |
| Module functions | 2 — `bal library funcs ballerina/graphql` |
| Errors | 9 — `AuthnError`, `AuthzError`, `ClientError`, `Error`, `HttpError`, `InvalidDocumentError`, `PayloadBindingError`, `RequestError`, `ServerError` — read one with `bal library type ballerina/graphql <Name>` |
| Types | 67 declarations (47 records, 6 type aliases, 1 enums, 5 constants, 5 classes and object types, 3 module-level variables), not listed here — read one with `type` |
| Guide | 833 lines — `bal library guide ballerina/graphql` |

Guide chunks (15): 1. `Create a standalone `graphql:Listener``  2. `Create a `graphql:Listener` using an `http:Listener``  3. `Service`  4. `Query type`  5. `Mutation type`  6. `Subscription Type`  7. `Scalar types`  8. `Enums`  9. `Record types`  10. `Service types`  11. `Arrays`  12. `Nullable types`  13. `Union types`  14. `Errors`  15. `Hierarchical resource paths` — `bal library guide ballerina/graphql <n>`

## Next

- `bal library client ballerina/graphql Client` — the client's whole callable surface
- `bal library class ballerina/graphql` — 5 classes and object types, called with `.`
- `bal library funcs ballerina/graphql` — 2 functions callable without a client
- `bal library overview ballerina/graphql -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerina/graphql <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 2 remote · `bal library client ballerina/graphql Client`

## Classes and object types — 5

- `Context` — 8 normal · `bal library class ballerina/graphql Context`
- `Field` — 7 normal · `bal library class ballerina/graphql Field`
- `Service` — nothing callable · `bal library class ballerina/graphql Service`
- `Interceptor` — 1 remote · `bal library class ballerina/graphql Interceptor`
- `Listener` — 5 normal · `bal library class ballerina/graphql Listener`

## Module-level functions — 2, call with `.`

```
__addError    getSdlString
```

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerina/graphql readme usage -->

```ballerina
import ballerina/graphql;

listener graphql:Listener graphqlListener = check new(4000);
```

```ballerina
import ballerina/graphql;
import ballerina/http;

listener http:Listener httpListener = check new(4000);
listener graphql:Listener graphqlListener = check new(httpListener);
```

```ballerina
import ballerina/graphql;

service graphql:Service /graphql on graphqlListener {
    // ...
}
```

```ballerina
import ballerina/graphql;

service graphql:Service on graphqlListener {
    // ...
}
```

```ballerina
service graphql:Service on new graphql:Listener(9090) {

}
```

```ballerina
import ballerina/graphql;

service graphql:Service /graphql on new graphql:Listener(4000) {
    resource function get greeting(string name) returns string {
        return "Hello, " + name;
    }
}
```

```ballerina
import ballerina/graphql;

public type Person record {|
    string name;
    int age;
    string city;
|};

service /graphql on new graphql:Listener(4000) {
    private Person profile;

    function init() {
        self.profile = { name: "Walter White", age: 50, city: "Albuquerque" };
    }

    resource function get profile() returns Person {
        return self.profile;
    }

    remote function updateName(string name) returns Person {
        self.profile.name = name;
        return self.profile;
    }

    remote function updateCity(string city) returns Person {
        self.profile.city = city;
        return self.profile;
    }
}
```

```ballerina
import ballerina/graphql;

service graphql:Service /graphql on new graphql:Listener(4000) {
    resource function subscribe messages() returns stream<string> {
        return ["Walter", "Jesse", "Mike"].toStream();
    }
}
```

```ballerina
resource function get greeting() returns string {
    return "Hello, World";
}
```

```ballerina
import ballerina/graphql;

public enum Color {
    RED,
    GREEN,
    BLUE
}

service on new graphql:Listener(4000) {
    resource function get color(int code) returns Color {
        // ...
    }
}
```

```ballerina
public type Person record {|
    string name;
    int age;
|};

resource function get profile() returns Person {
    return { name: "Walter White", age: 51 };
}
```

```ballerina
import ballerina/graphql;

service graphql:Service /graphql on new graphql:Listener(4000) {
    resource function get profile() returns Person {
        return new("Walter White", 51);
    }
}

service class Person {
    private string name;
    private int age;

    public function init(string name, int age) {
        self.name = name;
        self.age = age;
    }

    resource function get name() returns string => self.name;

    resource function get age() returns int => self.age;
}
```

```ballerina
public type Person record {|
    string name;
    int age;
|};

resource function get people() returns Person[] {
    Person p1 = { name: "Walter White", age: 51 };
    Person p2 = { name: "James Moriarty", age: 45 };
    Person p3 = { name: "Tom Marvolo Riddle", age: 71 };
    return [p1, p2, p3];
}
```

```ballerina
public type Person record {|
    string name;
    int age;
|};

resource function get profile(int id) returns Person? {
    if (id == 1) {
        return { name: "Walter White", age: 51 };
    }
}
```

```ballerina
import ballerina/graphql;

public type Profile Student|Teacher;

service /graphql on new graphql:Listener(4000) {
    resource function get profile(int purity) returns Profile {
        if (purity < 90) {
            return new Student(1, "Jesse Pinkman");
        } else {
            return new Teacher(737, "Walter White", "Chemistry");
        }
    }
}

distinct service class Student {
    private int id;
    private string name;

    public function init(int id, string name) {
        self.id = id;
        self.name = name;
    }

    resource function get id() returns int {
        return self.id;
    }

    resource function get name() returns string {
        return self.name;
    }
}

distinct service class Teacher {
    private int id;
    private string name;
    private string subject;

    public function init(int id, string name, string subject) {
        self.id = id;
        self.name = name;
        self.subject = subject;
    }

    resource function get id() returns int {
        return self.id;
    }

    resource function get name() returns string {
        return self.name;
    }

    resource function get subject() returns string {
        return self.subject;
    }
}
```

```ballerina
public type Person record {|
    string name;
    int age;
|};

resource function get profile(int id) returns Person|error {
    if (id == 1) {
        return { name: "Walter White", age: 51 };
    } else {
        return error(string `Invalid ID provided: ${id}`);
    }
}
```

```ballerina
import ballerina/graphql;

service graphql:Service /graphql on new graphq:Listener(4000) {
    resource function profile/name/first() returns string {
        return "Walter";
    }

    resource function profile/name/last() returns string {
        return "White"
    }

    resource function profile/age() returns int {
        return 51;
    }
}
```

<!-- guide: end ballerina/graphql readme usage -->
