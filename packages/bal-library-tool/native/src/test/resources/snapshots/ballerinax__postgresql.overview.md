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

## Quickstart

*Quoted from the package's own readme and checked against this version's declarations. A line marked `⚠` names something this version does not declare. The signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/postgresql readme usage -->

```ballerina
postgresql:Client|sql:Error dbClient = new ();
```

```ballerina
sql:VarcharValue name = new ("James");
sql:IntegerValue age = new (10);

sql:ParameterizedQuery query = `INSERT INTO student(age, name)
                                  VALUES (${age}, ${name})`;
sql:ExecutionResult result = check dbClient->execute(query);
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
int id = 10;
sql:ParameterizedQuery query = `SELECT * FROM students WHERE id = ${id}`;
Student retrievedStudent = check dbClient->queryRow(query);
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
listener postgresql:CdcListener cdcListener = new (database = {
    username: <username>,
    password: <password>,
    databaseName: "inventory"
});
```

<!-- guide: end ballerinax/postgresql readme usage -->

17 more examples — `bal library guide ballerinax/postgresql`

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
