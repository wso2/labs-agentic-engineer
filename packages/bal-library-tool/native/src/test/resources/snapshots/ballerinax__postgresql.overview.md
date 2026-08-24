<!-- bal library overview v1 -->
# ballerinax/postgresql 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/postgresql` |
| Classes | 126, too many to name here — `bal library class ballerinax/postgresql` |
| Module functions | none |
| Errors | none declared here; each operation names its error type in its `returns` clause |
| Types | 162 declarations (32 records, 4 enums, 126 classes and object types), not listed here — read one with `type` |
| Guide | 554 lines — `bal library guide ballerinax/postgresql` |

Guide chunks (18): 1. `Prerequisite`  2. `Change data capture`  3. `Create a client`  4. `Using SSL`  5. `Connection pool handling`  6. `Close the client`  7. `Parameterized query`  8. `Create tables`  9. `Insert data`  10. `Insert data with auto-generated keys`  11. `Query data`  12. `Update data`  13. `Delete data`  14. `Batch update data`  15. `Execute stored procedures`  16. `Create a listener`  17. `Configure the database connection`  18. `Implement a service to handle CDC events` — `bal library guide ballerinax/postgresql <n>`

## Next

- `bal library client ballerinax/postgresql Client` — the client's whole callable surface
- `bal library class ballerinax/postgresql` — 126 classes and object types, called with `.`
- `bal library overview ballerinax/postgresql -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/postgresql <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 5 remote, 1 normal · `bal library client ballerinax/postgresql Client`

## Classes and object types — 126

- `BitStringArrayValue` — nothing callable · `bal library class ballerinax/postgresql BitStringArrayValue`
- `BitStringValue` — nothing callable · `bal library class ballerinax/postgresql BitStringValue`
- `BoxArrayValue` — nothing callable · `bal library class ballerinax/postgresql BoxArrayValue`
- `BoxOutParameter` — 1 normal · `bal library class ballerinax/postgresql BoxOutParameter`
- `BoxValue` — nothing callable · `bal library class ballerinax/postgresql BoxValue`
- `ByteaOutParameter` — 1 normal · `bal library class ballerinax/postgresql ByteaOutParameter`
- `CidrArrayValue` — nothing callable · `bal library class ballerinax/postgresql CidrArrayValue`
- `CidrOutParameter` — 1 normal · `bal library class ballerinax/postgresql CidrOutParameter`
- `CidrValue` — nothing callable · `bal library class ballerinax/postgresql CidrValue`
- `CircleArrayValue` — nothing callable · `bal library class ballerinax/postgresql CircleArrayValue`
- `CircleOutParameter` — 1 normal · `bal library class ballerinax/postgresql CircleOutParameter`
- `CircleValue` — nothing callable · `bal library class ballerinax/postgresql CircleValue`
- `CustomResultIterator` — 2 normal · `bal library class ballerinax/postgresql CustomResultIterator`
- `CustomTypeValue` — nothing callable · `bal library class ballerinax/postgresql CustomTypeValue`
- `DateRangeArrayValue` — nothing callable · `bal library class ballerinax/postgresql DateRangeArrayValue`
- `DateRangeOutParameter` — 1 normal · `bal library class ballerinax/postgresql DateRangeOutParameter`
- `DateRangeValue` — nothing callable · `bal library class ballerinax/postgresql DateRangeValue`
- `EnumOutParameter` — 1 normal · `bal library class ballerinax/postgresql EnumOutParameter`
- `EnumValue` — nothing callable · `bal library class ballerinax/postgresql EnumValue`
- `InetArrayValue` — nothing callable · `bal library class ballerinax/postgresql InetArrayValue`

106 more, not listed — `bal library class ballerinax/postgresql -s "<what it does>"` searches all of them at once.

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/postgresql readme usage -->

```ballerina
import ballerinax/postgresql.driver as _;
```

```ballerina
postgresql:Client|sql:Error dbClient = new ();
```

```ballerina
postgresql:Client|sql:Error dbClient2 = 
                                new ("localhost", "postgres", "postgres", 
                                     "postgres", 5432);
```

```ballerina
postgresql:Options postgresqlOptions = {
  connectTimeout: 10
};
postgresql:Client|sql:Error dbClient = 
                                new (username = "postgres", password = "postgres", 
                                     database = "test", options = postgresqlOptions);
```

```ballerina
postgresql:Client|sql:Error dbClient4 = 
                                new (username = "postgres", password = "postgres",
                                     connectionPool = {maxOpenConnections: 5});
```

```ballerina
string clientStorePath = "/path/to/keystore.p12";

postgresql:Options postgresqlOptions = {
    ssl: {
        mode: postgresql:ALLOW,
        key: {
            path: clientStorePath,
            password: "ballerina"
        }
    }
};
```

```ballerina
postgresql:Client|sql:Error dbClient = 
                                    new (username = "postgres", password = "postgres", 
                                         database = "test");
```

```ballerina
postgresql:Client|sql:Error dbClient = 
                                    new (username = "postgres", password = "postgres", 
                                         database = "test", 
                                         connectionPool = { maxOpenConnections: 5 });
```

```ballerina
sql:ConnectionPool connPool = {maxOpenConnections: 5};
    
    postgresql:Client|sql:Error dbClient1 =
                                    new (username = "postgres", password = "postgres", 
                                    database = "test", connectionPool = connPool);
    postgresql:Client|sql:Error dbClient2 = 
                                    new (username = "postgres", password = "postgres", 
                                    database = "test", connectionPool = connPool);
    postgresql:Client|sql:Error dbClient3 = 
                                    new (username = "postgres", password = "postgres",
                                    database = "example", connectionPool = connPool);
```

```ballerina
error? e = dbClient.close();
```

```ballerina
check dbClient.close();
```

```ballerina
sql:ParameterizedQuery query = `SELECT * FROM students 
                                WHERE id < 10 AND age > 12`;
```

```ballerina
int[] ids = [10, 50];
int age = 12;
sql:ParameterizedQuery query = `SELECT * FROM students 
                                WHERE id < ${ids[0]} AND age > ${age}`;
```

```ballerina
int id = 10;
int age = 12;
sql:ParameterizedQuery query = `SELECT * FROM students`;
sql:ParameterizedQuery query1 = ` WHERE id < ${id} AND age > ${age}`;
sql:ParameterizedQuery sqlQuery = sql:queryConcat(query, query1);
```

```ballerina
int[] ids = [1, 2, 3];
sql:ParameterizedQuery query = `SELECT count(*) as total FROM DataTable 
                                WHERE row_id IN (${ids[0]}, ${ids[1]}, ${ids[2]})`;
```

```ballerina
int[] ids = [1, 2];
sql:ParameterizedQuery sqlQuery = 
                         sql:queryConcat(`SELECT * FROM DataTable WHERE id IN (`, 
                                          sql:arrayFlattenQuery(ids), `)`);
```

```ballerina
// Create the ‘Students’ table with the ‘id’, ’name’, and ’age’ fields.
sql:ExecutionResult result = 
                check dbClient->execute(`CREATE TABLE student (
                                           id INT SERIAL,
                                           age INT, 
                                           name VARCHAR(255), 
                                           PRIMARY KEY (id)
                                         )`);
// A value of the `sql:ExecutionResult` type is returned for the `result`.
```

```ballerina
sql:ExecutionResult result = check dbClient->execute(`INSERT INTO student(age, name)
                                                        VALUES (23, 'john')`);
```

```ballerina
string name = "Anne";
int age = 8;

sql:ParameterizedQuery query = `INSERT INTO student(age, name)
                                  VALUES (${age}, ${name})`;
sql:ExecutionResult result = check dbClient->execute(query);
```

```ballerina
sql:VarcharValue name = new ("James");
sql:IntegerValue age = new (10);

sql:ParameterizedQuery query = `INSERT INTO student(age, name)
                                  VALUES (${age}, ${name})`;
sql:ExecutionResult result = check dbClient->execute(query);
```

```ballerina
int age = 31;
string name = "Kate";

sql:ParameterizedQuery query = `INSERT INTO student(age, name)
                                  VALUES (${age}, ${name})`;
sql:ExecutionResult result = check dbClient->execute(query);

//Number of rows affected by the execution of the query.
int? count = result.affectedRowCount;

//The integer or string generated by the database in response to a query execution.
string|int? generatedKey = result.lastInsertId;
```

```ballerina
// Define an open record type to represent the results.
type Student record {
    int id;
    int age;
    string name;
};

// Select the data from the database table. The query parameters are passed 
// directly. Similar to the `execute` samples, parameters can be passed as
// sub types of `sql:TypedValue` as well.
int id = 10;
int age = 12;
sql:ParameterizedQuery query = `SELECT * FROM students
                                WHERE id < ${id} AND age > ${age}`;
stream<Student, sql:Error?> resultStream = dbClient->query(query);

// Iterating the returned table.
check from Student student in resultStream
    do {
       // Can perform operations using the `student` record of type `Student`.
    };
```

```ballerina
// Select the data from the database table. The query parameters are passed 
// directly. Similar to the `execute` samples, parameters can be passed as 
// sub types of `sql:TypedValue` as well.
int id = 10;
int age = 12;
sql:ParameterizedQuery query = `SELECT * FROM students
                                WHERE id < ${id} AND age > ${age}`;
stream<record{}, sql:Error?> resultStream = dbClient->query(query);

// Iterating the returned table.
check from record{} student in resultStream
    do {
        // Can perform operations using the `student` record.
        io:println("Student name: ", student.value["name"]);
    };
```

```ballerina
int id = 10;
sql:ParameterizedQuery query = `SELECT * FROM students WHERE id = ${id}`;
Student retrievedStudent = check dbClient->queryRow(query);
```

```ballerina
int age = 12;
sql:ParameterizedQuery query = `SELECT COUNT(*) FROM students WHERE age < ${age}`;
int youngStudents = check dbClient->queryRow(query);
```

```ballerina
int age = 23;
sql:ParameterizedQuery query = `UPDATE students SET name = 'John' WHERE age = ${age}`;
sql:ExecutionResult result = check dbClient->execute(query);
```

```ballerina
string name = "John";
sql:ParameterizedQuery query = `DELETE from students WHERE name = ${name}`;
sql:ExecutionResult result = check dbClient->execute(query);
```

```ballerina
// Create the table with the records that need to be inserted.
var data = [
  { name: "John", age: 25 },
  { name: "Peter", age: 24 },
  { name: "jane", age: 22 }
];

// Do the batch update by passing the batches.
sql:ParameterizedQuery[] batch = from var row in data
                                 select `INSERT INTO students ('name', 'age')
                                           VALUES (${row.name}, ${row.age})`;
sql:ExecutionResult[] result = check dbClient->batchExecute(batch);
```

```ballerina
int uid = 10;
sql:IntegerOutParameter insertId = new;

sql:ProcedureCallResult result = 
                         check dbClient->call(`call InsertPerson(${uid}, ${insertId})`);
stream<record{}, sql:Error?>? resultStr = result.queryResult;
if resultStr is stream<record{}, sql:Error?> {
    check from record{} result in resultStr
        do {
            // Can perform operations using the `result` record.
        };
}
check result.close();
```

```ballerina
listener postgresql:CdcListener cdcListener = new (database = {
    username: <username>,
    password: <password>,
    databaseName: "inventory"
});
```

```ballerina
listener postgresql:CdcListener cdcListener = new (database = {
    username: "cdc_user",
    password: "password",
    databaseName: "inventory",
    includedSchemas: ["public"],
    includedTables: ["public.products", "public.orders"],
    pluginName: postgresql:PGOUTPUT,
    slotName: "my_slot",
    publicationName: "my_publication",
    publicationAutocreateMode: postgresql:FILTERED
}, options = {
    heartbeatConfig: {
        interval: 10
    },
    guardrailConfig: {
        maxCollections: 100,
        limitAction: cdc:WARN
    }
});
```

```ballerina
service on cdcListener {
    remote function onRead(record{} after) returns cdc:Error? {
        io:println("Insert event: ", after);
    }

    remote function onCreate(record{} after) returns cdc:Error? {
        io:println("Insert event: ", after);
    }

    remote function onUpdate(record{} before, record{} after) returns cdc:Error? {
        io:println("Update event - Before: ", before, " After: ", after);
    }

    remote function onDelete(record{} before) returns error? {
        io:println("Delete event: ", before);
    }
}
```

<!-- guide: end ballerinax/postgresql readme usage -->
