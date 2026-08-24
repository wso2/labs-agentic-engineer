<!-- bal library client v1 -->
# Clients — ballerinax/postgresql `Client`

| | |
|---|---|
| Container | `Client` — 5 remote, 1 normal |
| Showing | 7 signatures |

## Next

- one call and every type it needs: `bal library client ballerinax/postgresql Client init -r`

## Constructor — 1

```ballerina
# Connects to a PostgreSQL database with the specified configuration.
isolated function init(string host = "localhost", string? username = "postgres", string? password = (), string? database = (), int port = 5432, Options? options = (), sql:ConnectionPool? connectionPool = ()) returns sql:Error?; // Special Agent Note: ConnectionPool, Error FROM ballerina/sql module
```

## Remote functions — 5, call with `->`

```ballerina
# Executes a SQL query and returns multiple results as a stream.
isolated remote function query(sql:ParameterizedQuery sqlQuery, typedesc<record {}> rowType = <>) returns stream<rowType, sql:Error?>; // Special Agent Note: ParameterizedQuery, Error FROM ballerina/sql module

# Executes a SQL query that is expected to return a single row or value as the result.
isolated remote function queryRow(sql:ParameterizedQuery sqlQuery, typedesc<anydata> returnType = <>) returns returnType|sql:Error; // Special Agent Note: ParameterizedQuery, Error FROM ballerina/sql module

# Executes a SQL query and returns execution metadata (not the actual query results).
# This function is typically used for operations like `INSERT`, `UPDATE`, or `DELETE`.
isolated remote function execute(sql:ParameterizedQuery sqlQuery) returns sql:ExecutionResult|sql:Error; // Special Agent Note: ParameterizedQuery, ExecutionResult, Error FROM ballerina/sql module

# Executes a SQL query with multiple sets of parameters in a single batch operation and returns execution metadata (not the actual query results).
# This function is typically used for batch operations like `INSERT`, `UPDATE`, or `DELETE`.
isolated remote function batchExecute(sql:ParameterizedQuery[] sqlQueries) returns sql:ExecutionResult[]|sql:Error; // Special Agent Note: ParameterizedQuery, ExecutionResult, Error FROM ballerina/sql module

# Calls a stored procedure with the given SQL query.
isolated remote function call(sql:ParameterizedCallQuery sqlQuery, typedesc<record {}>[] rowTypes = []) returns sql:ProcedureCallResult|sql:Error; // Special Agent Note: ParameterizedCallQuery, ProcedureCallResult, Error FROM ballerina/sql module
```

## Normal functions — 1, call with `.`

```ballerina
# Closes the PostgreSQL client and shuts down the connection pool.
# The client should be closed only at the end of the application lifetime, or when performing graceful stops in a service.
isolated function close() returns sql:Error?; // Special Agent Note: Error FROM ballerina/sql module
```
