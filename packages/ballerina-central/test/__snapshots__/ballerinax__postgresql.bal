// ============================================================
// Library: ballerinax/postgresql
// This module provides the functionality required to access and manipulate data stored in a PostgreSQL database. It enables seamless integration with PostgreSQL, supporting various data types and advanced database features.
// ============================================================
import ballerinax/postgresql;

// --- Types ---

# Represents the `Box` datatype in PostgreSQL.

type Box record {
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

type CertKey record {
    # The client certificate file
    string certFile;
    # The client private key file
    string keyFile;
    # Password of the private key if it is encrypted
    string keyPassword?;
};

# Represents the `Circle` datatype in PostgreSQL.

type Circle record {
    # The x coordinate of the center
    decimal x;
    # The y coordinate of the center
    decimal y;
    # The radius of the circle
    decimal r;
};

# Represents a user-defined datatype in PostgreSQL.

type CustomValueRecord record {
    # SQL type name
    string sqlTypeName;
    # List of values in the user-defined type
    CustomValues? values;
};

# Represents the values for user-defined datatypes in PostgreSQL.

type CustomValues record {
    # List of values in the user-defined type
    anydata[]? values;
};

# Represents the `Date Range` datatype in PostgreSQL.

type DateRange record {
    # Upper value in the range
    string upper;
    # Lower value in the range
    string lower;
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents the `Date (Record) Range` datatype in PostgreSQL.

type DateRecordRange record {
    # Upper value in the range
    time:Date upper; // Special Agent Note: Date FROM ballerina/time package
    # Lower value in the range
    time:Date lower; // Special Agent Note: Date FROM ballerina/time package
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents a value for `Enum` datatypes in PostgreSQL.

type Enum record {
    # Value for the Enum
    string value?;
};

# Represents the `Enum` datatype in PostgreSQL.

type EnumRecord record {
    # SQL type name
    string sqlTypeName;
    # Value for the Enum
    Enum? value;
};

# Represents the extended snapshot configuration for the PostgreSQL CDC listener.

type ExtendedSnapshotConfiguration record {
    # Table locking strategy during snapshot
    cdc:SnapshotLockingMode lockingMode; // Special Agent Note: SnapshotLockingMode FROM ballerinax/cdc package
    # Custom SELECT statements per table for filtering snapshot data
    string|string[] selectStatementOverrides;
    # Query strategy for snapshot execution
    cdc:SnapshotQueryMode queryMode; // Special Agent Note: SnapshotQueryMode FROM ballerinax/cdc package
    decimal delay;
    int fetchSize;
    int maxThreads;
    string|string[] includeCollectionList;
    cdc:IncrementalSnapshotConfiguration incrementalConfig; // Special Agent Note: IncrementalSnapshotConfiguration FROM ballerinax/cdc package
    # Lock acquisition timeout in seconds
    decimal lockTimeout = 10;
    # Transaction isolation level during snapshot
    cdc:SnapshotIsolationMode isolationMode?; // Special Agent Note: SnapshotIsolationMode FROM ballerinax/cdc package
};

# Represents the `Int4Range` datatype in PostgreSQL.

type IntegerRange record {
    # Upper value in the range
    int upper;
    # Lower value in the range
    int lower;
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents the `Interval` datatype in PostgreSQL.

type Interval record {
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

type Line record {
    # The a value in the standard line equation ax + by + c = 0
    decimal a;
    # The b value in the standard line equation ax + by + c = 0
    decimal b;
    # The c value in the standard line equation ax + by + c = 0
    decimal c;
};

# Represents the `Line Segment` datatype in PostgreSQL.

type LineSegment record {
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

type LongRange record {
    # Upper value in the range
    int upper;
    # Lower value in the range
    int lower;
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents the `NumRange` Datatype in PostgreSQL.

type NumericRange record {
    # Upper value in the range
    decimal upper;
    # Lower value in the range
    decimal lower;
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# An additional set of options related to the PostgreSQL database connection.

type Options record {
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
over to use server-side prepared statements. A value of 0 disables the cache.
    int preparedStatementThreshold?;
    # The number of queries that are cached in each connection
A value of 0 for preparedStatementThreshold disables the cache.
    int preparedStatementCacheQueries?;
    # The maximum size in mebibytes of the prepared queries
A value of 0 for preparedStatementThreshold disables the cache.
    int preparedStatementCacheSize?;
    # Time in seconds for sending the cancel command out of band over its own connection
    decimal cancelSignalTimeout = 10;
    # Enable or disable the TCP keep-alive probe
    boolean keepAliveTcpProbe?;
    # Use the binary format for sending and receiving data if possible
    boolean binaryTransfer?;
    # The schema to be used by the client
    string currentSchema?;
};

# Represents the `Path` datatype in PostgreSQL.

type Path record {
    # True if the path is open, false if closed
    boolean open = false;
    # The points defining this path
    Point[] points;
};

# Represents the `Point` datatype in PostgreSQL.

type Point record {
    # The X coordinate of the point
    decimal x;
    # The Y coordinate of the point
    decimal y;
};

# Represents the `Polygon` datatype in PostgreSQL.

type Polygon record {
    # The points defining the polygon
    Point[] points;
};

# Represents the configuration for the Postgres CDC database connection.

type PostgresDatabaseConnection record {
    # The class name of the PostgreSQL connector implementation to use
    string connectorClass = "io.debezium.connector.postgresql.PostgresConnector";
    # The hostname of the PostgreSQL server
    string hostname = "localhost";
    # The port number of the PostgreSQL server
    int port = 5432;
    # Database username for authentication
    string username;
    # Database password for authentication
    string password;
    # Connection timeout in seconds
    decimal connectTimeout;
    # The PostgreSQL connector always uses a single task and therefore does not use this value, so the default is always acceptable
    int tasksMax = 1;
    # SSL/TLS connection configuration
    cdc:SecureDatabaseConnection secure; // Special Agent Note: SecureDatabaseConnection FROM ballerinax/cdc package
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
    cdc:MessageKeyColumns[] messageKeyColumns?; // Special Agent Note: MessageKeyColumns FROM ballerinax/cdc package
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
};

# PostgreSQL CDC listener configuration including database connection, storage, and CDC options.

type PostgresListenerConfiguration record {
    # Database connection configuration (provided by DB-specific listener configs)
    cdc:DatabaseConnection database; // Special Agent Note: DatabaseConnection FROM ballerinax/cdc package
    # Debezium engine instance name
    string engineName;
    # Schema history storage configuration
    cdc:InternalSchemaStorage internalSchemaStorage; // Special Agent Note: InternalSchemaStorage FROM ballerinax/cdc package
    # Offset storage configuration
    cdc:OffsetStorage offsetStorage; // Special Agent Note: OffsetStorage FROM ballerinax/cdc package
    # Interval in seconds for checking CDC listener liveness
    decimal livenessInterval;
    # PostgreSQL-specific CDC options including snapshot, heartbeat, signals, and data type handling
    PostgreSqlOptions options = {};
};

# PostgreSQL-specific CDC options for configuring snapshot behavior and data type handling.

type PostgreSqlOptions record {
    # Initial snapshot behavior (initial, always, no_data, etc.)
    cdc:SnapshotMode snapshotMode; // Special Agent Note: SnapshotMode FROM ballerinax/cdc package
    # How to handle event processing failures
    cdc:EventProcessingFailureHandlingMode eventProcessingFailureHandlingMode; // Special Agent Note: EventProcessingFailureHandlingMode FROM ballerinax/cdc package
    # Database operations to skip publishing
    cdc:Operation[] skippedOperations; // Special Agent Note: Operation FROM ballerinax/cdc package
    # Whether to discard events with no data changes
    boolean skipMessagesWithoutChange;
    # Representation mode for decimal values
    cdc:DecimalHandlingMode decimalHandlingMode; // Special Agent Note: DecimalHandlingMode FROM ballerinax/cdc package
    # Maximum number of events in the internal queue
    int maxQueueSize;
    # Maximum number of events per processing batch
    int maxBatchSize;
    # Database query timeout in seconds
    decimal queryTimeout;
    # Heartbeat configuration for keeping the PostgreSQL replication slot active
    cdc:RelationalHeartbeatConfiguration heartbeatConfig?; // Special Agent Note: RelationalHeartbeatConfiguration FROM ballerinax/cdc package
    # Signal channel configuration for ad-hoc control
    cdc:SignalConfiguration signalConfig; // Special Agent Note: SignalConfiguration FROM ballerinax/cdc package
    # Transaction boundary event configuration
    cdc:TransactionMetadataConfiguration transactionMetadataConfig; // Special Agent Note: TransactionMetadataConfiguration FROM ballerinax/cdc package
    # Column masking and transformation configuration
    cdc:ColumnTransformConfiguration columnTransformConfig; // Special Agent Note: ColumnTransformConfiguration FROM ballerinax/cdc package
    # Topic naming and routing configuration
    cdc:TopicConfiguration topicConfig; // Special Agent Note: TopicConfiguration FROM ballerinax/cdc package
    # Error handling and retry configuration
    cdc:ConnectionRetryConfiguration connectionRetryConfig; // Special Agent Note: ConnectionRetryConfiguration FROM ballerinax/cdc package
    # Performance tuning configuration
    cdc:PerformanceConfiguration performanceConfig; // Special Agent Note: PerformanceConfiguration FROM ballerinax/cdc package
    # Extended snapshot configuration with PostgreSQL-specific lock timeout and query settings
    ExtendedSnapshotConfiguration extendedSnapshot?;
    # Data type handling configuration including schema change tracking
    cdc:DataTypeConfiguration dataTypeConfig?; // Special Agent Note: DataTypeConfiguration FROM ballerinax/cdc package
};

# PostgreSQL publication configuration (pgoutput plugin).

type PublicationConfiguration record {
    # Name of PostgreSQL publication
    string publicationName = "dbz_publication";
    # Mode for auto-creating publications
    PublicationAutocreateMode publicationAutocreateMode = ALL_TABLES;
};

# Represents the `Int4Range` datatype in PostgreSQL.

type Range record {
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

type ReplicationConfiguration record {
    # Logical decoding plugin to use (pgoutput, decoderbufs)
    PostgreSQLLogicalDecodingPlugin pluginName = PGOUTPUT;
    # Name of the PostgreSQL logical replication slot
    string slotName = "debezium";
    # Drop replication slot when connector stops
    boolean slotDropOnStop = false;
    # Custom replication slot parameters
    string slotStreamParams?;
};

# The SSL configurations to be used when connecting to the PostgreSQL server.

type SecureSocket record {
    # The `postgresql:SSLMode` to be used during the connection
    SSLMode mode = PREFER;
    # File name of the SSL root certificate. Defaults to the `defaultdir/root.crt`.
in which the `defaultdir` is `${user.home}/.postgresql/` in Unix systems and
`%appdata%/postgresql/` on Windows.
    string rootcert?;
    # Keystore configuration of the client certificates
    crypto:KeyStore|CertKey key?; // Special Agent Note: KeyStore FROM ballerina/crypto package
};

# PostgreSQL streaming and status configuration.

type StreamingConfiguration record {
    # Interval for sending status updates to PostgreSQL in seconds
    decimal statusUpdateInterval = 10;
    # Interval for fetching current xmin position in seconds
    decimal xminFetchInterval = 0;
    # LSN flushing strategy
    LsnFlushMode lsnFlushMode?;
};

# Represents the `Timestamp (Civil) Range` datatype in PostgreSQL.

type TimestampCivilRange record {
    # Upper value in the range
    time:Civil upper; // Special Agent Note: Civil FROM ballerina/time package
    # Lower value in the range
    time:Civil lower; // Special Agent Note: Civil FROM ballerina/time package
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents the `Timestamp Range` datatype in PostgreSQL.

type TimestampRange record {
    # Upper value in the range
    string upper;
    # Lower value in the range
    string lower;
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents the `Timestamp (Civil) with Timezone Range` Datatype in PostgreSQL.

type TimestamptzCivilRange record {
    # Upper value in the range
    time:Civil upper; // Special Agent Note: Civil FROM ballerina/time package
    # Lower value in the range
    time:Civil lower; // Special Agent Note: Civil FROM ballerina/time package
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents the `Timestamp with Timezone Range` datatype in PostgreSQL.

type TimestamptzRange record {
    # Upper value in the range
    string upper;
    # Lower value in the range
    string lower;
    # True if upper value is included in the range
    boolean upperboundInclusive;
    # True if lower value is included in the range
    boolean lowerboundInclusive;
};

# Represents LSN flush modes.
enum LsnFlushMode {
    MANUAL,
    CONNECTOR,
    CONNECTOR_AND_DRIVER
}

# Represents the PostgreSQL logical decoding plugins.
enum PostgreSQLLogicalDecodingPlugin {
    PGOUTPUT,
    DECODERBUFS
}

# Represents publication autocreate modes.
enum PublicationAutocreateMode {
    ALL_TABLES,
    DISABLED,
    FILTERED
}

# Possible values for the SSL mode.
enum SSLMode {
    PREFER,
    REQUIRE,
    DISABLE,
    ALLOW,
    VERIFY_CA,
    VERIFY_FULL
}

class BitStringArrayValue {
}

class BitStringValue {
}

class BoxArrayValue {
}

class BoxOutParameter {
}

class BoxValue {
}

class ByteaOutParameter {
}

class CidrArrayValue {
}

class CidrOutParameter {
}

class CidrValue {
}

class CircleArrayValue {
}

class CircleOutParameter {
}

class CircleValue {
}

class CustomResultIterator {
}

class CustomTypeValue {
}

class DateRangeArrayValue {
}

class DateRangeOutParameter {
}

class DateRangeValue {
}

class EnumOutParameter {
}

class EnumValue {
}

class InetArrayValue {
}

class InetOutParameter {
}

class InetValue {
}

class InOutParameter {
}

class IntegerRangeArrayValue {
}

class IntegerRangeOutParameter {
}

class IntegerRangeValue {
}

class IntervalArrayValue {
}

class IntervalOutParameter {
}

class IntervalValue {
}

class JsonArrayValue {
}

class JsonBinaryArrayValue {
}

class JsonBinaryValue {
}

class JsonbOutParameter {
}

class JsonOutParameter {
}

class JsonPathArrayValue {
}

class JsonPathOutParameter {
}

class JsonPathValue {
}

class JsonValue {
}

class LineArrayValue {
}

class LineOutParameter {
}

class LineSegmentArrayValue {
}

class LineSegmentValue {
}

class LineValue {
}

class LongRangeArrayValue {
}

class LongRangeOutParameter {
}

class LongRangeValue {
}

class LsegOutParameter {
}

class MacAddr8ArrayValue {
}

class MacAddr8OutParameter {
}

class MacAddr8Value {
}

class MacAddrArrayValue {
}

class MacAddrOutParameter {
}

class MacAddrValue {
}

class MoneyArrayValue {
}

class MoneyOutParameter {
}

class MoneyValue {
}

class NumericRangeArrayValue {
}

class NumericRangeOutParameter {
}

class NumericRangeValue {
}

class PathArrayValue {
}

class PathOutParameter {
}

class PathValue {
}

class PGBitArrayValue {
}

class PGBitOutParameter {
}

class PGBitValue {
}

class PglsnArrayValue {
}

class PglsnOutParameter {
}

class PglsnValue {
}

class PGXmlArrayValue {
}

class PGXmlOutParameter {
}

class PGXmlValue {
}

class PointArrayValue {
}

class PointOutParameter {
}

class PointValue {
}

class PolygonArrayValue {
}

class PolygonOutParameter {
}

class PolygonValue {
}

class RegClassArrayValue {
}

class RegClassOutParameter {
}

class RegClassValue {
}

class RegConfigArrayValue {
}

class RegConfigOutParameter {
}

class RegConfigValue {
}

class RegDictionaryArrayValue {
}

class RegDictionaryOutParameter {
}

class RegDictionaryValue {
}

class RegNamespaceArrayValue {
}

class RegNamespaceOutParameter {
}

class RegNamespaceValue {
}

class RegOperArrayValue {
}

class RegOperatorArrayValue {
}

class RegOperatorOutParameter {
}

class RegOperatorValue {
}

class RegOperOutParameter {
}

class RegOperValue {
}

class RegProcArrayValue {
}

class RegProcedureArrayValue {
}

class RegProcedureOutParameter {
}

class RegProcedureValue {
}

class RegProcOutParameter {
}

class RegProcValue {
}

class RegRoleArrayValue {
}

class RegRoleOutParameter {
}

class RegRoleValue {
}

class RegTypeArrayValue {
}

class RegTypeOutParameter {
}

class RegTypeValue {
}

class TimestampRangeOutParameter {
}

class TimestampTzRangeOutParameter {
}

class TsQueryArrayValue {
}

class TsQueryOutParameter {
}

class TsQueryValue {
}

class TsRangeArrayValue {
}

class TsRangeValue {
}

class TsTzRangeArrayValue {
}

class TsTzRangeValue {
}

class TsVectorArrayValue {
}

class TsVectorOutParameter {
}

class TsVectorValue {
}

class UuidArrayValue {
}

class UuidOutParameter {
}

class UuidValue {
}

class VarBitStringArrayValue {
}

class VarBitStringOutParameter {
}

class VarBitStringValue {
}

// --- Client ---

# PostgreSQL database client that enables interaction with PostgreSQL servers and supports standard SQL operations.
client class Client {
    function init(string host = "localhost", string? username = "postgres", string? password = (), string? database = (), int port = 5432, Options? options = (), sql:ConnectionPool? connectionPool = ()) returns sql:Error?; // Special Agent Note: ConnectionPool, Error FROM ballerina/sql package

    # Executes a SQL query and returns multiple results as a stream.
    remote function query(sql:ParameterizedQuery sqlQuery, typedesc<record {}> rowType = <>) returns stream<rowType,sql:Error?>; // Special Agent Note: ParameterizedQuery, Error FROM ballerina/sql package

    # Executes a SQL query that is expected to return a single row or value as the result.
    remote function queryRow(sql:ParameterizedQuery sqlQuery, typedesc<anydata> returnType = <>) returns returnType|sql:Error; // Special Agent Note: ParameterizedQuery, Error FROM ballerina/sql package

    # Executes a SQL query and returns execution metadata (not the actual query results).
    # This function is typically used for operations like `INSERT`, `UPDATE`, or `DELETE`.
    remote function execute(sql:ParameterizedQuery sqlQuery) returns sql:ExecutionResult|sql:Error; // Special Agent Note: ParameterizedQuery, ExecutionResult, Error FROM ballerina/sql package

    # Executes a SQL query with multiple sets of parameters in a single batch operation and returns execution metadata (not the actual query results).
    # This function is typically used for batch operations like `INSERT`, `UPDATE`, or `DELETE`.
    remote function batchExecute(sql:ParameterizedQuery[] sqlQueries) returns sql:ExecutionResult[]|sql:Error; // Special Agent Note: ParameterizedQuery, ExecutionResult, Error FROM ballerina/sql package

    # Calls a stored procedure with the given SQL query.
    remote function call(sql:ParameterizedCallQuery sqlQuery, typedesc<record {}>[] rowTypes = []) returns sql:ProcedureCallResult|sql:Error; // Special Agent Note: ParameterizedCallQuery, ProcedureCallResult, Error FROM ballerina/sql package

    # Closes the PostgreSQL client and shuts down the connection pool.
    # The client should be closed only at the end of the application lifetime, or when performing graceful stops in a service.
    function close() returns sql:Error?; // Special Agent Note: Error FROM ballerina/sql package
}
