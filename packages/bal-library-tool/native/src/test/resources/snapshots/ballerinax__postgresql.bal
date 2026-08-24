// ============================================================
// Library: ballerinax/postgresql
// This module provides the functionality required to access and manipulate data stored in a PostgreSQL database. It enables seamless integration with PostgreSQL, supporting various data types and advanced database features.
// ============================================================
import ballerinax/postgresql;

// --- Types ---

# Represents the `Box` datatype in PostgreSQL.
public type Box record {
    # The x ccordinate of a corner of the box
    decimal x1;
    # The y ccordinate of a corner of the box
    decimal y1;
    # The x cocrdinate of the opposite corner of the box
    decimal x2;
    # The y cocrdinate of the opposite corner of the box
    decimal y2;
};

# Represents the combination of the certificate, the private key, and the private key password if encrypted
public type CertKey record {|
    # The client certificate file
    string certFile;
    # The client private key file
    string keyFile;
    # Password of the private key if it is encrypted
    string keyPassword?;
|};

# Represents the `Circle` datatype in PostgreSQL.
public type Circle record {
    # The x coordinate of the center
    decimal x;
    # The y coordinate of the center
    decimal y;
    # The radius of the circle
    decimal r;
};

# Represents a user-defined datatype in PostgreSQL.
public type CustomValueRecord record {
    # SQL type name
    string sqlTypeName;
    # List of values in the user-defined type
    CustomValues? values;
};

# Represents the values for user-defined datatypes in PostgreSQL.
public type CustomValues record {
    # List of values in the user-defined type
    anydata[]? values;
};

# Represents the `Date Range` datatype in PostgreSQL.
public type DateRange record {
    *Range;
    # Upper value in the range
    string upper;
    # Lower value in the range
    string lower;
};

# Represents the `Date (Record) Range` datatype in PostgreSQL.
public type DateRecordRange record {
    *Range;
    # Upper value in the range
    time:Date upper; // Special Agent Note: Date FROM ballerina/time module
    # Lower value in the range
    time:Date lower; // Special Agent Note: Date FROM ballerina/time module
};

# Represents a value for `Enum` datatypes in PostgreSQL.
public type Enum record {
    # Value for the Enum
    string value?;
};

# Represents the `Enum` datatype in PostgreSQL.
public type EnumRecord record {
    # SQL type name
    string sqlTypeName;
    # Value for the Enum
    Enum? value;
};

# Represents the extended snapshot configuration for the PostgreSQL CDC listener.
public type ExtendedSnapshotConfiguration record {|
    *cdc:RelationalExtendedSnapshotConfiguration; // Special Agent Note: RelationalExtendedSnapshotConfiguration FROM ballerinax/cdc module
    # Lock acquisition timeout in seconds
    decimal lockTimeout = 10;
    # Transaction isolation level during snapshot
    cdc:SnapshotIsolationMode isolationMode?; // Special Agent Note: SnapshotIsolationMode FROM ballerinax/cdc module
|};

# Represents the `Int4Range` datatype in PostgreSQL.
public type IntegerRange record {
    *Range;
    # Upper value in the range
    int upper;
    # Lower value in the range
    int lower;
};

# Represents the `Interval` datatype in PostgreSQL.
public type Interval record {
    # Number of years in the interval
    int years = 0;
    # Number of months in the interval
    int months = 0;
    # Number of days in the interval
    int days = 0;
    # Number of hours in the interval
    int hours = 0;
    # Number of minutes in the interval
    int minutes = 0;
    # Number of seconds in the interval
    decimal seconds = 0;
};

# Represents the `Line` datatype in PostgreSQL.
public type Line record {
    # The a value in the standard line equation ax + by + c = 0
    decimal a;
    # The b value in the standard line equation ax + by + c = 0
    decimal b;
    # The c value in the standard line equation ax + by + c = 0
    decimal c;
};

# Represents the `Line Segment` datatype in PostgreSQL.
public type LineSegment record {
    # The X coordinate of the first point of the line segment
    decimal x1;
    # The Y coordinate of the first point of the line segment
    decimal y1;
    # The X coordinate of the second point of the line segment
    decimal x2;
    # The Y coordinate of the second point of the line segment
    decimal y2;
};

# Represents the `Int8Range` datatype in PostgreSQL.
public type LongRange record {
    *Range;
    # Upper value in the range
    int upper;
    # Lower value in the range
    int lower;
};

# Represents the `NumRange` Datatype in PostgreSQL.
public type NumericRange record {
    *Range;
    # Upper value in the range
    decimal upper;
    # Lower value in the range
    decimal lower;
};

# An additional set of options related to the PostgreSQL database connection.
public type Options record {|
    # SSL configurations to be used
    SecureSocket ssl?;
    # Timeout in seconds for connecting to the server
    decimal connectTimeout = 0;
    # Socket timeout in seconds for read/write operations with the server (0 means no socket timeout)
    decimal socketTimeout = 0;
    # Timeout in seconds for connecting to the server and authentication (0 means no timeout)
    decimal loginTimeout = 0;
    # The number of rows to be fetched in one trip to the database
    int rowFetchSize?;
    # The maximum number of fields to be cached per connection. A value of 0 disables the cache
    int cachedMetadataFieldsCount?;
    # The maximum size in megabytes of fields to be cached per connection. A value of 0 disables the cache
    int cachedMetadataFieldSize?;
    # The number of `PreparedStatement` executions required before switching
    # over to use server-side prepared statements. A value of 0 disables the cache.
    int preparedStatementThreshold?;
    # The number of queries that are cached in each connection
    # A value of 0 for preparedStatementThreshold disables the cache.
    int preparedStatementCacheQueries?;
    # The maximum size in mebibytes of the prepared queries
    # A value of 0 for preparedStatementThreshold disables the cache.
    int preparedStatementCacheSize?;
    # Time in seconds for sending the cancel command out of band over its own connection
    decimal cancelSignalTimeout = 10;
    # Enable or disable the TCP keep-alive probe
    boolean keepAliveTcpProbe?;
    # Use the binary format for sending and receiving data if possible
    boolean binaryTransfer?;
    # The schema to be used by the client
    string currentSchema?;
|};

# Represents the `Path` datatype in PostgreSQL.
public type Path record {
    # True if the path is open, false if closed
    boolean open = false;
    # The points defining this path
    Point[] points;
};

# Represents the `Point` datatype in PostgreSQL.
public type Point record {
    # The X coordinate of the point
    decimal x;
    # The Y coordinate of the point
    decimal y;
};

# Represents the `Polygon` datatype in PostgreSQL.
public type Polygon record {
    # The points defining the polygon
    Point[] points;
};

# Represents the configuration for the Postgres CDC database connection.
public type PostgresDatabaseConnection record {|
    *cdc:DatabaseConnection; // Special Agent Note: DatabaseConnection FROM ballerinax/cdc module
    # The class name of the PostgreSQL connector implementation to use
    string connectorClass = "io.debezium.connector.postgresql.PostgresConnector";
    # The hostname of the PostgreSQL server
    string hostname = "localhost";
    # The port number of the PostgreSQL server
    int port = 5432;
    # The name of the PostgreSQL database from which to stream the changes.
    string databaseName;
    # A list of regular expressions matching fully-qualified schema identifiers to capture changes from
    string|string[] includedSchemas?;
    # A list of regular expressions matching fully-qualified schema identifiers to exclude from change capture
    string|string[] excludedSchemas?;
    # Regex patterns for tables to capture (mutually exclusive with `excludedTables`)
    string|string[] includedTables?;
    # Regex patterns for tables to exclude (mutually exclusive with `includedTables`)
    string|string[] excludedTables?;
    # Regex patterns for columns to capture (mutually exclusive with `excludedColumns`)
    string|string[] includedColumns?;
    # Regex patterns for columns to exclude (mutually exclusive with `includedColumns`)
    string|string[] excludedColumns?;
    # Composite message key columns for change events
    cdc:MessageKeyColumns[] messageKeyColumns?; // Special Agent Note: MessageKeyColumns FROM ballerinax/cdc module
    # The PostgreSQL connector always uses a single task and therefore does not use this value, so the default is always acceptable
    int tasksMax = 1;
    # Deprecated: Use `replicationConfig.pluginName` instead.
    PostgreSQLLogicalDecodingPlugin pluginName = PGOUTPUT;
    # Deprecated: Use `replicationConfig.slotName` instead.
    string slotName = "debezium";
    # Deprecated: Use `publicationConfig.publicationName` instead.
    string publicationName = "dbz_publication";
    # Replication configuration (logical decoding plugin, slot name and parameters). Takes priority over deprecated top-level fields
    ReplicationConfiguration replicationConfig?;
    # Publication configuration (publication name and autocreate mode). Takes priority over deprecated top-level fields
    PublicationConfiguration publicationConfig?;
    StreamingConfiguration streamingConfig?;
|};

# PostgreSQL CDC listener configuration including database connection, storage, and CDC options.
public type PostgresListenerConfiguration record {|
    # PostgreSQL database connection, logical decoding, and capture settings
    PostgresDatabaseConnection database;
    *cdc:ListenerConfiguration; // Special Agent Note: ListenerConfiguration FROM ballerinax/cdc module
    # PostgreSQL-specific CDC options including snapshot, heartbeat, signals, and data type handling
    PostgreSqlOptions options = {};
|};

# PostgreSQL-specific CDC options for configuring snapshot behavior and data type handling.
public type PostgreSqlOptions record {|
    *cdc:Options; // Special Agent Note: Options FROM ballerinax/cdc module
    # Extended snapshot configuration with PostgreSQL-specific lock timeout and query settings
    ExtendedSnapshotConfiguration extendedSnapshot?;
    # Data type handling configuration including schema change tracking
    cdc:DataTypeConfiguration dataTypeConfig?; // Special Agent Note: DataTypeConfiguration FROM ballerinax/cdc module
    # Heartbeat configuration for keeping the PostgreSQL replication slot active
    cdc:RelationalHeartbeatConfiguration heartbeatConfig?; // Special Agent Note: RelationalHeartbeatConfiguration FROM ballerinax/cdc module
|};

# PostgreSQL publication configuration (pgoutput plugin).
public type PublicationConfiguration record {|
    # Name of PostgreSQL publication
    string publicationName = "dbz_publication";
    # Mode for auto-creating publications
    PublicationAutocreateMode publicationAutocreateMode = ALL_TABLES;
|};

# Represents the `Int4Range` datatype in PostgreSQL.
public type Range record {
    # Upper value in the range
    anydata upper;
    # Lower value in the range
    anydata lower;
    # True if upper value is included in the range
    boolean upperboundInclusive = false;
    # True if lower value is included in the range
    boolean lowerboundInclusive = false;
};

# PostgreSQL replication configuration (logical decoding).
public type ReplicationConfiguration record {|
    # Logical decoding plugin to use (pgoutput, decoderbufs)
    PostgreSQLLogicalDecodingPlugin pluginName = PGOUTPUT;
    # Name of the PostgreSQL logical replication slot
    string slotName = "debezium";
    # Drop replication slot when connector stops
    boolean slotDropOnStop = false;
    # Custom replication slot parameters
    string slotStreamParams?;
|};

# The SSL configurations to be used when connecting to the PostgreSQL server.
public type SecureSocket record {|
    # The `postgresql:SSLMode` to be used during the connection
    SSLMode mode = PREFER;
    # File name of the SSL root certificate. Defaults to the `defaultdir/root.crt`.
    # in which the `defaultdir` is `${user.home}/.postgresql/` in Unix systems and
    # `%appdata%/postgresql/` on Windows.
    string rootcert?;
    # Keystore configuration of the client certificates
    crypto:KeyStore|CertKey key?; // Special Agent Note: KeyStore FROM ballerina/crypto module
|};

# PostgreSQL streaming and status configuration.
public type StreamingConfiguration record {|
    # Interval for sending status updates to PostgreSQL in seconds
    decimal statusUpdateInterval = 10;
    # Interval for fetching current xmin position in seconds
    decimal xminFetchInterval = 0;
    # LSN flushing strategy
    LsnFlushMode lsnFlushMode?;
|};

# Represents the `Timestamp (Civil) Range` datatype in PostgreSQL.
public type TimestampCivilRange record {
    *Range;
    # Upper value in the range
    time:Civil upper; // Special Agent Note: Civil FROM ballerina/time module
    # Lower value in the range
    time:Civil lower; // Special Agent Note: Civil FROM ballerina/time module
};

# Represents the `Timestamp Range` datatype in PostgreSQL.
public type TimestampRange record {
    *Range;
    # Upper value in the range
    string upper;
    # Lower value in the range
    string lower;
};

# Represents the `Timestamp (Civil) with Timezone Range` Datatype in PostgreSQL.
public type TimestamptzCivilRange record {
    *Range;
    # Upper value in the range
    time:Civil upper; // Special Agent Note: Civil FROM ballerina/time module
    # Lower value in the range
    time:Civil lower; // Special Agent Note: Civil FROM ballerina/time module
};

# Represents the `Timestamp with Timezone Range` datatype in PostgreSQL.
public type TimestamptzRange record {
    *Range;
    # Upper value in the range
    string upper;
    # Lower value in the range
    string lower;
};

# Represents LSN flush modes.
public enum LsnFlushMode {
    MANUAL,
    CONNECTOR,
    CONNECTOR_AND_DRIVER
}

# Represents the PostgreSQL logical decoding plugins.
public enum PostgreSQLLogicalDecodingPlugin {
    # The standard logical decoding output plug-in in PostgreSQL 10+
    PGOUTPUT,
    # A logical decoding plugin based on Protobuf and maintained by the Debezium community
    DECODERBUFS
}

# Represents publication autocreate modes.
public enum PublicationAutocreateMode {
    ALL_TABLES,
    DISABLED,
    FILTERED
}

# Possible values for the SSL mode.
public enum SSLMode {
    PREFER,
    REQUIRE,
    DISABLE,
    ALLOW,
    VERIFY_CA,
    VERIFY_FULL
}

# Represents the `Bit(n)` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class BitStringArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Bit(n)` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class BitStringValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Box` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class BoxArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Box?[]|string?[] value;

    isolated function init(Box?[]|string?[] value = <string?[]>[]);
}

# Represents the `Box` `OutParameter` in `sql:ParameterizedCallQuery`.
public class BoxOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Box` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class BoxValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Box|string? value;

    isolated function init(Box|string? value = ());
}

# Represents the `Bytea range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class ByteaOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Cidr` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class CidrArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Cidr` `OutParameter` in `sql:ParameterizedCallQuery`.
public class CidrOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Cidr` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class CidrValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Circle` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class CircleArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Circle?[]|string?[] value;

    isolated function init(Circle?[]|string?[] value = <string?[]>[]);
}

# Represents the `Circle` `OutParameter` in `sql:ParameterizedCallQuery`.
public class CircleOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Circle` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class CircleValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Circle|string? value;

    isolated function init(Circle|string? value = ());
}

# The iterator for the stream returned in `query` function to be used in overriding the default behaviour of `sql:ResultIterator`.
public class CustomResultIterator {
    # Retrieves the next result from the `sql:ResultIterator`.
    # + iterator - The `sql:ResultIterator` to fetch the next result from.
    # + return - A record containing the next result, or an `sql:Error` if an error occurs.
    isolated function nextResult(sql:ResultIterator iterator) returns record {}|sql:Error?; // Special Agent Note: ResultIterator, Error FROM ballerina/sql module

    # Retrieves the next query result from the `sql:ProcedureCallResult`.
    # + callResult - The `sql:ProcedureCallResult` to fetch the next query result from.
    # + return - `true` if there is a next query result, `false` if there are no more results, or an `sql:Error` if an error occurs.
    isolated function getNextQueryResult(sql:ProcedureCallResult callResult) returns boolean|sql:Error; // Special Agent Note: ProcedureCallResult, Error FROM ballerina/sql module
}

# Represents the user-defined PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class CustomTypeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public CustomValueRecord value;

    isolated function init(string sqlTypeName, CustomValues? value = ());
}

# Represents the `Date range` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class DateRangeArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public DateRange?[]|DateRecordRange?[]|string?[] value;

    isolated function init(DateRange?[]|DateRecordRange?[]|string?[] value = <string?[]>[]);
}

# Represents the `Date Range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class DateRangeOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Date range` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class DateRangeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public DateRange|DateRecordRange|string? value;

    isolated function init(DateRange|DateRecordRange|string? value = ());
}

# Represents the `Enum` `OutParameter` in `sql:ParameterizedCallQuery`.
public class EnumOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Enum` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class EnumValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public EnumRecord value;

    isolated function init(string sqlTypeName, Enum? value = ());
}

# Represents the `Inet` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class InetArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Inet` `OutParameter` in `sql:ParameterizedCallQuery`.
public class InetOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Inet` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class InetValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the PostgreSQL `InOutParameter` in `sql:ParameterizedCallQuery`.
public class InOutParameter {
    public sql:Value 'in; // Special Agent Note: Value FROM ballerina/sql module

    isolated function init(sql:Value 'in); // Special Agent Note: Value FROM ballerina/sql module

    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Int4 range` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class IntegerRangeArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public IntegerRange?[]|string?[] value;

    isolated function init(IntegerRange?[]|string?[] value = <string?[]>[]);
}

# Represents the `Int4 range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class IntegerRangeOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Int4 range` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class IntegerRangeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public IntegerRange|string? value;

    isolated function init(IntegerRange|string? value = ());
}

# Represents the `Time interval` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class IntervalArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Interval?[]|string?[] value;

    isolated function init(Interval?[]|string?[] value = <string?[]>[]);
}

# Represents the `Interval` `OutParameter` in `sql:ParameterizedCallQuery`.
public class IntervalOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Time interval` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class IntervalValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Interval|string? value;

    isolated function init(Interval|string? value = ());
}

# Represents the `JSON` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class JsonArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public json[]|string?[] value;

    isolated function init(json[]|string?[] value = <string?[]>[]);
}

# Represents the `JSONB` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class JsonBinaryArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public json[]|string?[] value;

    isolated function init(json[]|string?[] value = <string?[]>[]);
}

# Represents the `JSONB` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class JsonBinaryValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public json|string? value;

    isolated function init(json|string? value = ());
}

# Represents the `JSONB` `OutParameter` in `sql:ParameterizedCallQuery`.
public class JsonbOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `JSON` `OutParameter` in `sql:ParameterizedCallQuery`.
public class JsonOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `JSONPath` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class JsonPathArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `JSONPath` `OutParameter` in `sql:ParameterizedCallQuery`.
public class JsonPathOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `JSONPath` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class JsonPathValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `JSON` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class JsonValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public json|string? value;

    isolated function init(json|string? value = ());
}

# Represents the `Line` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class LineArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Line?[]|string?[] value;

    isolated function init(Line?[]|string?[] value = <string?[]>[]);
}

# Represents the `Line` `OutParameter` in `sql:ParameterizedCallQuery`.
public class LineOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Line` segment array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class LineSegmentArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public LineSegment?[]|string?[] value;

    isolated function init(LineSegment?[]|string?[] value = <string?[]>[]);
}

# Represents the `Line` segment PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class LineSegmentValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public LineSegment|string? value;

    isolated function init(LineSegment|string? value = ());
}

# Represents the `Line` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class LineValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Line|string? value;

    isolated function init(Line|string? value = ());
}

# Represents the `Int8 range` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class LongRangeArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public LongRange?[]|string?[] value;

    isolated function init(LongRange?[]|string?[] value = <string?[]>[]);
}

# Represents the `Int8 Range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class LongRangeOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Int8 range` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class LongRangeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public LongRange|string? value;

    isolated function init(LongRange|string? value = ());
}

# Represents the `Lseg` `OutParameter` in `sql:ParameterizedCallQuery`.
public class LsegOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Macaddress8` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class MacAddr8ArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `MacAddr8` `OutParameter` in `sql:ParameterizedCallQuery`.
public class MacAddr8OutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Macaddress8` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class MacAddr8Value {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Macaddress` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class MacAddrArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `MacAddr` `OutParameter` in `sql:ParameterizedCallQuery`.
public class MacAddrOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Macaddress` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class MacAddrValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Money` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class MoneyArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public decimal?[]|float?[]|string?[] value;

    isolated function init(decimal?[]|float?[]|string?[] value = <string?[]>[]);
}

# Represents the `Money` `OutParameter` in `sql:ParameterizedCallQuery`.
public class MoneyOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Money` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class MoneyValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public decimal|float|string? value;

    isolated function init(decimal|float|string? value = ());
}

# Represents the `Numerical range` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class NumericRangeArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public NumericRange?[]|string?[] value;

    isolated function init(NumericRange?[]|string?[] value = <string?[]>[]);
}

# Represents the `Numeric range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class NumericRangeOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Numerical range` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class NumericRangeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public NumericRange|string? value;

    isolated function init(NumericRange|string? value = ());
}

# Represents the `Path` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PathArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Path?[]|Point[]?[]|string?[] value;

    isolated function init(Path?[]|Point[]?[]|string?[] value = <string?[]>[]);
}

# Represents the `Path` `OutParameter` in `sql:ParameterizedCallQuery`.
public class PathOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Path` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PathValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Path|Point[]|string? value;

    isolated function init(Path|Point[]|string? value = ());
}

# Represents the `Bit` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PGBitArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public boolean?[]|string?[] value;

    isolated function init(boolean?[]|string?[] value = <string?[]>[]);
}

# Represents the `PGBit` `OutParameter` in `sql:ParameterizedCallQuery`.
public class PGBitOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Bit` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PGBitValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public boolean|string? value;

    isolated function init(boolean|string? value = ());
}

# Represents the `pg_lsn` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PglsnArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Pglsn` `OutParameter` in `sql:ParameterizedCallQuery`.
public class PglsnOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Pg_lsn` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PglsnValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regtype` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PGXmlArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[]|xml?[] value;

    isolated function init(string?[]|xml?[] value = <string?[]>[]);
}

# Represents the `XML range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class PGXmlOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regtype` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PGXmlValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string|xml? value;

    isolated function init(string|xml? value = ());
}

# Represents the `Point` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PointArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Point?[]|string?[] value;

    isolated function init(Point[]|string?[] value = <string?[]>[]);
}

# Represents the `Point` `OutParameter` in `sql:ParameterizedCallQuery`.
public class PointOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Point` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PointValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Point|string? value;

    isolated function init(Point|string? value = ());
}

# Represents the `Polygon` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PolygonArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Point[]?[]|string?[] value;

    isolated function init(Point[]?[]|string?[] value = <string?[]>[]);
}

# Represents the `Polygon` `OutParameter` in `sql:ParameterizedCallQuery`.
public class PolygonOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Polygon` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class PolygonValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public Point[]|string? value;

    isolated function init(Point[]|string? value = ());
}

# Represents the `regclass` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegClassArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regclass` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegClassOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regclass` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegClassValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regconfig` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegConfigArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regconfig` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegConfigOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regconfig` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegConfigValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regdictionary` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegDictionaryArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regdictionary` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegDictionaryOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regdictionary` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegDictionaryValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regnamespace` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegNamespaceArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regnamespace` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegNamespaceOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regnamespace` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegNamespaceValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regoper` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegOperArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `regoperator` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegOperatorArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regoperator` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegOperatorOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regoperator` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegOperatorValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Regoper` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegOperOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regoper` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegOperValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regproc` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegProcArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `regprocedure` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegProcedureArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regprocedure` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegProcedureOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regprocedure` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegProcedureValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Regproc` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegProcOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regproc` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegProcValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regrole` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegRoleArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regrole` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegRoleOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regrole` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegRoleValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `regtype` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegTypeArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Regtype` `OutParameter` in `sql:ParameterizedCallQuery`.
public class RegTypeOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `regtype` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class RegTypeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Timestamp Range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class TimestampRangeOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Timestamp with Timezone Range` `OutParameter` in `sql:ParameterizedCallQuery`.
public class TimestampTzRangeOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Text query` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsQueryArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Text Query` `OutParameter` in `sql:ParameterizedCallQuery`.
public class TsQueryOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Text query` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsQueryValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Timestamp range` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsRangeArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public TimestampRange?[]|TimestampCivilRange?[]|string?[] value;

    isolated function init(TimestampRange?[]|TimestampCivilRange?[]|string?[] value = <string?[]>[]);
}

# Represents the `Timestamp range` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsRangeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public TimestampRange|TimestampCivilRange|string? value;

    isolated function init(TimestampRange|TimestampCivilRange|string? value = ());
}

# Represents the `Timestamp with timezone range` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsTzRangeArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public TimestamptzRange?[]|TimestamptzCivilRange?[]|string?[] value;

    isolated function init(TimestamptzRange?[]|TimestamptzCivilRange?[]|string?[] value = <string?[]>[]);
}

# Represents the `Timestamp with timezone range` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsTzRangeValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public TimestamptzRange|TimestamptzCivilRange|string? value;

    isolated function init(TimestamptzRange|TimestamptzCivilRange|string? value = ());
}

# Represents the `Text vector` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsVectorArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Text Vector` `OutParameter` in `sql:ParameterizedCallQuery`.
public class TsVectorOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Text vector` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class TsVectorValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `UUID` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class UuidArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `UUID` `OutParameter` in `sql:ParameterizedCallQuery`.
public class UuidOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `UUID` PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class UuidValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

# Represents the `Bit vary(n)` array PostgreSQL type parameter in `sql:ParameterizedQuery`.
public class VarBitStringArrayValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string?[] value;

    isolated function init(string?[] value = []);
}

# Represents the `Varbitstring` `OutParameter` in `sql:ParameterizedCallQuery`.
public class VarBitStringOutParameter {
    # Parses the returned SQL value to a Ballerina value.
    # + typeDesc - The `typedesc` of the type to which the result needs to be returned
    # + return - The result in the `typeDesc` type, or an `sql:Error`
    isolated function get(typedesc<anydata> typeDesc = <>) returns typeDesc|sql:Error; // Special Agent Note: Error FROM ballerina/sql module
}

# Represents the `Bit vary(n) PostgreSQL` type parameter in `sql:ParameterizedQuery`.
public class VarBitStringValue {
    *sql:TypedValue; // Special Agent Note: TypedValue FROM ballerina/sql module
    # Value of the parameter
    public string? value;

    isolated function init(string? value = ());
}

// --- Client ---

# PostgreSQL database client that enables interaction with PostgreSQL servers and supports standard SQL operations.
public isolated client class Client {
    # Connects to a PostgreSQL database with the specified configuration.
    # + host - PostgreSQL server hostname
    # + password - Database password
    # + database - Database name to connect to. The default is to connect to a database with the
    # same name as the username
    # + port - PostgreSQL server port
    # + options - The advanced connection options specific to the PostgreSQL database.
    # + connectionPool - The `sql:ConnectionPool` object to be used within the client. If not provided, the global connection pool (shared by all clients) will be used
    # + return - An `sql:Error` if the client creation fails
    isolated function init(string host = "localhost", string? username = "postgres", string? password = (), string? database = (), int port = 5432, Options? options = (), sql:ConnectionPool? connectionPool = ()) returns sql:Error?; // Special Agent Note: ConnectionPool, Error FROM ballerina/sql module

    # Executes a SQL query and returns multiple results as a stream.
    # + sqlQuery - The SQL query as `sql:ParameterizedQuery` (e.g., `` `SELECT * FROM users WHERE id=${userId}` ``)
    # + rowType - The `typedesc` of the record type to which the result needs to be mapped
    # + return - Stream of records containing the query results. Please ensure that the stream is fully consumed, or close the stream.
    isolated remote function query(sql:ParameterizedQuery sqlQuery, typedesc<record {}> rowType = <>) returns stream<rowType, sql:Error?>; // Special Agent Note: ParameterizedQuery, Error FROM ballerina/sql module

    # Executes a SQL query that is expected to return a single row or value as the result.
    # + sqlQuery - The SQL query as `sql:ParameterizedQuery` (e.g., `` `SELECT * from Album WHERE name=${albumName}` ``)
    # + returnType - The `typedesc` of the anydata (record or basic type) to which the result needs to be returned.
    # It can be a basic type if the query result contains only one column
    # + return - The result of the query or an `sql:Error`.
    # - If the query does not return any results, an `sql:NoRowsError` is returned.
    # - If the query returns multiple rows, only the first row is returned.
    isolated remote function queryRow(sql:ParameterizedQuery sqlQuery, typedesc<anydata> returnType = <>) returns returnType|sql:Error; // Special Agent Note: ParameterizedQuery, Error FROM ballerina/sql module

    # Executes a SQL query and returns execution metadata (not the actual query results).
    # This function is typically used for operations like `INSERT`, `UPDATE`, or `DELETE`.
    # + sqlQuery - The SQL query as `sql:ParameterizedQuery` (e.g., `` `DELETE FROM Album WHERE artist=${artistName}` ``)
    # + return - The execution metadata as an `sql:ExecutionResult`, or an `sql:Error` if execution fails
    isolated remote function execute(sql:ParameterizedQuery sqlQuery) returns sql:ExecutionResult|sql:Error; // Special Agent Note: ParameterizedQuery, ExecutionResult, Error FROM ballerina/sql module

    # Executes a SQL query with multiple sets of parameters in a single batch operation and returns execution metadata (not the actual query results).
    # This function is typically used for batch operations like `INSERT`, `UPDATE`, or `DELETE`.
    # + sqlQueries - The SQL query with multiple sets of parameters as an array of `sql:ParameterizedQuery`
    # + return - The execution metadata as an array of `sql:ExecutionResult` or an `sql:Error`. If one of the commands in the batch fails, an `sql:BatchExecuteError` will be returned immediately
    isolated remote function batchExecute(sql:ParameterizedQuery[] sqlQueries) returns sql:ExecutionResult[]|sql:Error; // Special Agent Note: ParameterizedQuery, ExecutionResult, Error FROM ballerina/sql module

    # Calls a stored procedure with the given SQL query.
    # + sqlQuery - The SQL query to call the procedure as `sql:ParameterizedQuery` (e.g., `` `CALL get_user(${id})` ``)
    # + rowTypes - An array of `typedesc` of the record type to which the result needs to be mapped
    # + return - The summary of the execution and results are returned in an `sql:ProcedureCallResult`, or an `sql:Error`. Once the results are processed, invoke the `close` method on the `sql:ProcedureCallResult`.
    isolated remote function call(sql:ParameterizedCallQuery sqlQuery, typedesc<record {}>[] rowTypes = []) returns sql:ProcedureCallResult|sql:Error; // Special Agent Note: ParameterizedCallQuery, ProcedureCallResult, Error FROM ballerina/sql module

    # Closes the PostgreSQL client and shuts down the connection pool.
    # The client should be closed only at the end of the application lifetime, or when performing graceful stops in a service.
    # + return - `sql:Error` if closing the client fails, else `()`
    isolated function close() returns sql:Error?; // Special Agent Note: Error FROM ballerina/sql module
}

// --- Listeners ---

# Represents the Ballerina Postgresql CDC Listener.
public isolated class CdcListener {
    # Starts the CDC listener.
    isolated function 'start() returns cdc:Error|(); // Special Agent Note: Error FROM ballerinax/cdc module

    # Initializes the Postgresql listener with the given configuration.
    # + config - The configuration for the Postgresql connector
    isolated function init(*PostgresListenerConfiguration config);

    # Attaches a CDC service to the Postgresql listener.
    # + s - The CDC service to attach
    # + name - Attachment points
    # + return - An `cdc:Error` if the service cannot be attached, or `()` if successful
    isolated function attach(cdc:Service s, string[]|string? name = ()) returns cdc:Error?; // Special Agent Note: Service, Error FROM ballerinax/cdc module

    # Detaches a CDC service from the Postgresql listener.
    # + s - The CDC service to detach
    # + return - An `cdc:Error` if the service cannot be detached, or `()` if successful
    isolated function detach(cdc:Service s) returns cdc:Error?; // Special Agent Note: Service, Error FROM ballerinax/cdc module

    # Stops the Postgresql listener gracefully.
    # + return - An `cdc:Error` if the listener cannot be stopped, or `()` if successful
    isolated function gracefulStop() returns cdc:Error?; // Special Agent Note: Error FROM ballerinax/cdc module

    # Stops the Postgresql listener immediately.
    # + return - An `cdc:Error` if the listener cannot be stopped, or `()` if successful
    isolated function immediateStop() returns cdc:Error?; // Special Agent Note: Error FROM ballerinax/cdc module
}
