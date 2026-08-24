// ============================================================
// Library: ballerinax/kafka
// Apache Kafka is a distributed event streaming platform used for high-performance data pipelines, streaming analytics, data integration, and mission-critical applications. The Kafka connector allows you to integrate with Kafka, providing the ability to produce and consume events from Kafka topics. It supports high-throughput, fault-tolerant, and scalable messaging, making it ideal for building real-time data processing systems.
// ============================================================
import ballerinax/kafka;

// --- Types ---

# Type related to anydata consumer record.
public type AnydataConsumerRecord record {|
    # Key that is included in the record
    anydata key?;
    # Anydata record content
    anydata value;
    # Timestamp of the record, in milliseconds since epoch
    int timestamp;
    # Topic partition position in which the consumed record is stored
    PartitionOffset offset;
    # Map of headers included with the record
    map<byte[]|byte[][]|string|string[]> headers;
|};

# Details related to the anydata producer record.
public type AnydataProducerRecord record {|
    # Topic to which the record will be appended
    string topic;
    # Key that is included in the record
    anydata key?;
    # Anydata record content
    anydata value;
    # Timestamp of the record, in milliseconds since epoch
    int timestamp?;
    # Partition to which the record should be sent
    int partition?;
    # Map of headers to be included with the record
    map<byte[]|byte[][]|string|string[]> headers?;
|};

# Configurations related to Kafka authentication mechanisms.
public type AuthenticationConfiguration record {|
    # Type of the authentication mechanism. Currently `SASL_PLAIN`, `SASL_SCRAM_256` & `SASL_SCRAM_512`
    # is supported
    AuthenticationMechanism mechanism = AUTH_SASL_PLAIN;
    # The username to authenticate the Kafka producer/consumer
    string username;
    # The password to authenticate the Kafka producer/consumer
    string password;
|};

# Subtype related to `kafka:AnydataConsumerRecord` record.
public type BytesConsumerRecord record {|
    *AnydataConsumerRecord;
    # Record content in bytes
    byte[] value;
    # Headers as a byte[] or byte[][]
    map<byte[]|byte[][]> headers;
|};

# Subtype related to `kafka:AnydataProducerRecord` record.
public type BytesProducerRecord record {|
    *AnydataProducerRecord;
    # Record content in bytes
    byte[] value;
    # Headers as a byte[] or byte[][]
    map<byte[]|byte[][]> headers?;
|};

# Represents a combination of certificate, private key, and private key password if encrypted.
public type CertKey record {|
    # A file containing the certificate
    string certFile;
    # A file containing the private key in PKCS8 format
    string keyFile;
    # Password of the private key if it is encrypted
    string keyPassword?;
|};

# Configurations related to consumer endpoint.
public type ConsumerConfiguration record {|
    # Unique string that identifies the consumer
    string groupId?;
    # Topics to be subscribed by the consumer
    string|string[] topics?;
    # Offset reset strategy if no initial offset
    OffsetResetMethod offsetReset?;
    # Strategy class for handling the partition assignment among consumers
    string partitionAssignmentStrategy?;
    # Metrics recording level
    string metricsRecordingLevel?;
    # Metrics reporter classes
    string metricsReporterClasses?;
    # Identifier to be used for server side logging
    string clientId?;
    # Interceptor classes to be used before sending the records
    string interceptorClasses?;
    # Transactional message reading method
    IsolationLevel isolationLevel?;
    # Avro schema registry URL. Use this field to specify the schema registry URL, if the Avro serializer
    # is used
    string schemaRegistryUrl?;
    # Configurations to initialize a schema registry
    readonly map<anydata> schemaRegistryConfig?;
    # Key deserialization type
    DeserializerType keyDeserializerType = DES_BYTE_ARRAY;
    # Value deserialization type
    DeserializerType valueDeserializerType = DES_BYTE_ARRAY;
    # Additional properties for the property fields not provided by the Ballerina `kafka` module. Use
    # this with caution since this can override any of the fields. It is not recomendded to use
    # this field except in an extreme situation
    map<string> additionalProperties?;
    # Timeout (in seconds) used to detect consumer failures when the heartbeat threshold is reached
    decimal sessionTimeout?;
    # Expected time (in seconds) between the heartbeats
    decimal heartBeatInterval?;
    # Maximum time (in seconds) to force a refresh of metadata
    decimal metadataMaxAge?;
    # Auto committing interval (in seconds) for commit offset when auto-committing is enabled
    decimal autoCommitInterval?;
    # The maximum amount of data the server returns per partition
    int maxPartitionFetchBytes?;
    # Size of the TCP send buffer (SO_SNDBUF)
    int sendBuffer?;
    # Size of the TCP receive buffer (SO_RCVBUF)
    int receiveBuffer?;
    # Minimum amount of data the server should return for a fetch request
    int fetchMinBytes?;
    # Maximum amount of data the server should return for a fetch request
    int fetchMaxBytes?;
    # Maximum amount of time (in seconds) the server will block before answering the fetch request
    decimal fetchMaxWaitTime?;
    # Maximum amount of time in seconds to wait when reconnecting
    decimal reconnectBackoffTimeMax?;
    # Time (in seconds) to wait before attempting to retry a failed request
    decimal retryBackoff?;
    # Window of time (in seconds) a metrics sample is computed over
    decimal metricsSampleWindow?;
    # Number of samples maintained to compute metrics
    int metricsNumSamples?;
    # Wait time (in seconds) for response of a request
    decimal requestTimeout?;
    # Close idle connections after the number of seconds
    decimal connectionMaxIdleTime?;
    # Maximum number of records returned in a single call to poll
    int maxPollRecords?;
    # Maximum delay between invocations of poll
    int maxPollInterval?;
    # Time (in seconds) to wait before attempting to reconnect
    decimal reconnectBackoffTime?;
    # Timeout interval for polling in seconds
    decimal pollingTimeout?;
    # Polling interval for the consumer in seconds
    decimal pollingInterval?;
    # Number of concurrent consumers
    int concurrentConsumers?;
    # Default API timeout value (in seconds) for APIs with duration
    decimal defaultApiTimeout?;
    # Enables auto committing offsets
    boolean autoCommit = true;
    # Checks the CRC32 of the records consumed. This ensures that no on-the-wire or on-disk corruption occurred
    # to the messages. This may add some overhead and might need to be set to `false` if extreme
    # performance is required
    boolean checkCRCS = true;
    # Whether records from internal topics should be exposed to the consumer
    boolean excludeInternalTopics = true;
    # Decouples processing
    boolean decoupleProcessing = false;
    # Configuration related to constraint validation check
    boolean validation = true;
    # Automatically seeks past the erroneous records in the event of an data-binding or
    # validating constraints failure
    boolean autoSeekOnValidationFailure = true;
    # Configurations related to SSL/TLS encryption
    SecureSocket secureSocket?;
    # Authentication-related configurations for the `kafka:Consumer`
    AuthenticationConfiguration auth?;
    # Type of the security protocol to use in the broker connection
    SecurityProtocol securityProtocol = PROTOCOL_PLAINTEXT;
|};

# Defines the Payload remote function parameter.
public type KafkaPayload record {|
|};

# Represents an offset and a timestamp for a topic partition.
public type OffsetAndTimestamp record {|
    # The offset of the record in the topic partition
    int offset;
    # The timestamp of the record in the topic partition
    int timestamp;
    # The leader epoch of the record in the topic partition
    int? leaderEpoch = ();
|};

# Represents the topic partition position in which the consumed record is stored.
public type PartitionOffset record {|
    # The `kafka:TopicPartition` to which the record is related
    TopicPartition partition;
    # Offset in which the record is stored in the partition
    int offset;
|};

# Represents the `kafka:Producer` configuration.
public type ProducerConfiguration record {|
    # Number of acknowledgments
    ProducerAcks acks = ACKS_SINGLE;
    # Compression type to be used for messages
    CompressionType compressionType = COMPRESSION_NONE;
    # Identifier to be used for server side logging
    string clientId?;
    # Metrics recording level
    string metricsRecordingLevel?;
    # Metrics reporter classes
    string metricReporterClasses?;
    # Partitioner class to be used to select the partition to which the message is sent
    string partitionerClass?;
    # Interceptor classes to be used before sending the records
    string interceptorClasses?;
    # Transactional ID to be used in transactional delivery
    string transactionalId?;
    # Avro schema registry URL. Use this field to specify the schema registry URL if the Avro
    # serializer is used
    string schemaRegistryUrl?;
    # The schema used for key/value serialization (Deprecated)
    string avroSchema?;
    # The schema used to serializate the key
    string keySchema?;
    # The schema used to serialize the value
    string valueSchema?;
    # Configurations to initialize a schema registry
    map<anydata> schemaRegistryConfig?;
    # Key serialization type
    SerializerType keySerializerType = SER_BYTE_ARRAY;
    # Value serialization type
    SerializerType valueSerializerType = SER_BYTE_ARRAY;
    # Additional properties for the property fields not provided by the Ballerina `kafka` module. Use
    # this with caution since this can override any of the fields. It is not recomendded to use
    # this field except in an extreme situation
    map<string> additionalProperties?;
    # Total bytes of memory the producer can use to buffer records
    int bufferMemory?;
    # Number of retries to resend a record
    int retryCount?;
    # Maximum number of bytes to be batched together when sending the records. Records exceeding this limit will
    # not be batched. Setting this to 0 will disable batching
    int batchSize?;
    # Delay (in seconds) to allow other records to be batched before sending them to the Kafka server
    decimal linger?;
    # Size of the TCP send buffer (SO_SNDBUF)
    int sendBuffer?;
    # Size of the TCP receive buffer (SO_RCVBUF)
    int receiveBuffer?;
    # The maximum size of a request in bytes
    int maxRequestSize?;
    # Time (in seconds) to wait before attempting to reconnect
    decimal reconnectBackoffTime?;
    # Maximum amount of time in seconds to wait when reconnecting
    decimal reconnectBackoffMaxTime?;
    # Time (in seconds) to wait before attempting to retry a failed request
    decimal retryBackoffTime?;
    # Maximum block time (in seconds) during which the sending is blocked when the buffer is full
    decimal maxBlock?;
    # Wait time (in seconds) for the response of a request
    decimal requestTimeout?;
    # Maximum time (in seconds) to force a refresh of metadata
    decimal metadataMaxAge?;
    # Time (in seconds) window for a metrics sample to compute over
    decimal metricsSampleWindow?;
    # Number of samples maintained to compute the metrics
    int metricsNumSamples?;
    # Maximum number of unacknowledged requests on a single connection
    int maxInFlightRequestsPerConnection?;
    # Close the idle connections after this number of seconds
    decimal connectionsMaxIdleTime?;
    # Timeout (in seconds) for transaction status update from the producer
    decimal transactionTimeout?;
    # Exactly one copy of each message is written to the stream when enabled
    boolean enableIdempotence = false;
    # Configurations related to SSL/TLS encryption
    SecureSocket secureSocket?;
    # Authentication-related configurations for the `kafka:Producer`
    AuthenticationConfiguration auth?;
    # Type of the security protocol to use in the broker connection
    SecurityProtocol securityProtocol = PROTOCOL_PLAINTEXT;
|};

# Represents metadata of a Kafka record.
public type RecordMetadata record {|
    # The offset of the record in the topic partition
    int? offset = ();
    # The timestamp of the record in the topic partition
    int? timestamp = ();
    # The size of the serialized, uncompressed key in bytes. If key is null, the returned size is -1.
    int serializedKeySize;
    # The size of the serialized, uncompressed value in bytes. If value is null, the returned size is -1.
    int serializedValueSize;
    # The topic the record is appended to
    string topic;
    # The partition the record is sent to
    int partition;
|};

# Configurations for secure communication with the Kafka server.
public type SecureSocket record {|
    # Configurations associated with crypto:TrustStore or single certificate file that the client trusts
    crypto:TrustStore|string cert; // Special Agent Note: TrustStore FROM ballerina/crypto module
    # Configurations associated with crypto:KeyStore or combination of certificate and private key of the client
    record {crypto:KeyStore keyStore; string keyPassword; }|CertKey key?;
    # SSL/TLS protocol related options
    record {Protocol name; string[] versions; } protocol?;
    # List of ciphers to be used. By default, all the available cipher suites are supported
    string[] ciphers?;
    # Name of the security provider used for SSL connections. The default value is the default security provider
    # of the JVM
    string provider?;
|};

# Represents a topic partition.
public type TopicPartition record {|
    # Topic to which the partition is related
    string topic;
    # Index of the specific partition
    int partition;
|};

# Defines the common error type for the module.
public type Error distinct error;

# Represents an error, which occurred due to payload binding.
public type PayloadBindingError distinct (Error & error<PartitionOffset>);

# Represents an error, which occurred due to payload constraint validation.
public type PayloadValidationError distinct (PayloadBindingError & error<PartitionOffset>);

# Producer acknowledgement type is 'all'. This will guarantee that the record will not be lost as long as at least one
# in-sync replica is alive.
public const string ACKS_ALL = "all";

# Producer acknowledgement type '0'. If the acknowledgement type set to this, the producer will not wait for any
# acknowledgement from the server.
public const string ACKS_NONE = "0";

# Producer acknowledgement type '1'. If the acknowledgement type set to this, the leader will write the record to its
# A local log will respond without waiting FOR full acknowledgement from all the followers.
public const string ACKS_SINGLE = "1";

# Kafka SASL_PLAIN authentication mechanism
public const string AUTH_SASL_PLAIN = "PLAIN";

# Kafka SASL_SCRAM authentication mechanism
public const string AUTH_SASL_SCRAM_SHA_256 = "SCRAM-SHA-256";

public const string AUTH_SASL_SCRAM_SHA_512 = "SCRAM-SHA-512";

# Kafka GZIP compression type.
public const string COMPRESSION_GZIP = "gzip";

# Kafka LZ4 compression type.
public const string COMPRESSION_LZ4 = "lz4";

# No compression.
public const string COMPRESSION_NONE = "none";

# Kafka Snappy compression type.
public const string COMPRESSION_SNAPPY = "snappy";

# Kafka ZSTD compression type.
public const string COMPRESSION_ZSTD = "zstd";

# The default server URL.
public const string DEFAULT_URL = "localhost:9092";

# User-defined deserializer.
public const string DES_CUSTOM = "CUSTOM";

# In-built Kafka float deserializer.
public const string DES_FLOAT = "FLOAT";

# In-built Kafka int deserializer.
public const string DES_INT = "INT";

# In-built Kafka byte array deserializer.
# In-built Kafka string deserializer.
public const string DES_STRING = "STRING";

# Configures the consumer to read the committed messages only in the transactional mode when poll() is called.
public const string ISOLATION_COMMITTED = "read_committed";

# Configures the consumer to read all the messages including the aborted ones.
public const string ISOLATION_UNCOMMITTED = "read_uncommitted";

# Automatically reset the consumer offset to the earliest offset
public const string OFFSET_RESET_EARLIEST = "earliest";

# Automatically reset the consumer offset to the latest offset
public const string OFFSET_RESET_LATEST = "latest";

# If the `offsetReset` is set to `OFFSET_RESET_NONE`, the consumer will give an error if no previous offset is found
# for the consumer group
public const string OFFSET_RESET_NONE = "none";

# Represents Kafka un-authenticated, non-encrypted channel
public const string PROTOCOL_PLAINTEXT = "PLAINTEXT";

# Represents Kafka authenticated, non-encrypted channel
public const string PROTOCOL_SASL_PLAINTEXT = "SASL_PLAINTEXT";

# Represents Kafka SASL authenticated, SSL channel
public const string PROTOCOL_SASL_SSL = "SASL_SSL";

# Represents Kafka SSL channel
public const string PROTOCOL_SSL = "SSL";

# User-defined serializer.
public const string SER_CUSTOM = "CUSTOM";

# In-built Kafka float serializer.
public const string SER_FLOAT = "FLOAT";

# In-built Kafka int serializer.
public const string SER_INT = "INT";

# In-built Kafka string serializer.
public const string SER_STRING = "STRING";

# Kafka in-built deserializer type.
public enum DeserializerType {
    DES_BYTE_ARRAY,
    DES_AVRO
}

# Represents protocol options.
public enum Protocol {
    SSL,
    TLS,
    DTLS
}

# Kafka in-built serializer types.
public enum SerializerType {
    SER_BYTE_ARRAY,
    SER_AVRO
}

# Implementation of the `Deserializer` interface for Avro deserialization
public isolated class AvroDeserializer {
    isolated function init(anydata & readonly schemaRegistryConfig) returns error?;

    isolated function deserialize(byte[] value) returns anydata|error;
}

# Implementation of the `Serializer` interface for Avro serialization
public isolated class AvroSerializer {
    isolated function init(anydata & readonly schemaRegistryConfig, string schema) returns error?;

    isolated function serialize(anydata value, string schema, string subject) returns byte[]|error;
}

# Interface for deserializing a given value
public type Deserializer object {
    # Deserializes the provided value
    # + value - Data to be deserialized
    # + return - Deserialized value as `anydata` on success, otherwise an error
    function deserialize(byte[] value) returns anydata|error;
};

# Interface for serializing a given value
public type Serializer object {
    # Serializes a given value using the provided schema
    # + value - Data to be serialized
    # + schema - The schema used for serialization
    # + subject - The subject under which the schema is registered (default: "subject")
    # + return - The serialized `byte[]` on success, otherwise an error
    isolated function serialize(anydata value, string schema, string subject) returns byte[]|error;
};

# The Kafka service type.
public type Service distinct service object {
};

# Represents the different types of offset-reset methods of the Kafka consumer.
public type OffsetResetMethod OFFSET_RESET_EARLIEST|OFFSET_RESET_LATEST|OFFSET_RESET_NONE;

# `kafka:Consumer` isolation level type.
public type IsolationLevel ISOLATION_COMMITTED|ISOLATION_UNCOMMITTED;

# `kafka:Producer` acknowledgement types.
public type ProducerAcks ACKS_ALL|ACKS_NONE|ACKS_SINGLE;

# Kafka compression types to compress the messages.
public type CompressionType COMPRESSION_NONE|COMPRESSION_GZIP|COMPRESSION_SNAPPY|COMPRESSION_LZ4|COMPRESSION_ZSTD;

# Represents the supported Kafka SASL authentication mechanisms.
public type AuthenticationMechanism AUTH_SASL_PLAIN|AUTH_SASL_SCRAM_SHA_256|AUTH_SASL_SCRAM_SHA_512;

# Represents the supported security protocols for Kafka clients.
public type SecurityProtocol PROTOCOL_PLAINTEXT|PROTOCOL_SASL_PLAINTEXT|PROTOCOL_SASL_SSL|PROTOCOL_SSL;

# Represents a topic partition and a timestamp.
public type TopicPartitionTimestamp [TopicPartition, int];

# Represents a topic partition and an offset with a timestamp.
public type TopicPartitionOffset [TopicPartition, OffsetAndTimestamp?];

// --- Client ---

# Represents a Kafka caller, which can be used to commit the offsets consumed by the service.
public isolated client class Caller {
    # Commits the currently consumed offsets of the service.
    # ```ballerina
    # kafka:Error? result = caller->commit();
    # ```
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated remote function 'commit() returns Error?;

    # Commits the given offsets and partitions for the given topics of the service.
    # ```ballerina
    # kafka:Error? result = caller->commitOffset([partitionOffset1, partitionOffset2]);
    # ```
    # + offsets - Offsets to be committed
    # + duration - Timeout duration (in seconds) for the commit operation execution
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function commitOffset(PartitionOffset[] offsets, decimal duration = -1) returns Error?;

    # Seeks for a given offset in a topic partition.
    # ```ballerina
    # kafka:Error? result = consumer->seek(partitionOffset);
    # ```
    # + offset - The `PartitionOffset` to seek
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function seek(PartitionOffset offset) returns Error?;
}

# Represents a Kafka consumer endpoint.
public isolated client class Consumer {
    # Creates a new `kafka:Consumer`.
    # + bootstrapServers - List of remote server endpoints of Kafka brokers
    # + config - Configurations related to the consumer endpoint
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated function init(string|string[] bootstrapServers, *ConsumerConfiguration config) returns Error?;

    # Assigns consumer to a set of topic partitions.
    # ```ballerina
    # kafka:Error? result = consumer->assign([topicPartition1, topicPartition2]);
    # ```
    # + partitions - Topic partitions to be assigned
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function assign(TopicPartition[] partitions) returns Error?;

    # Closes the consumer connection with the external Kafka broker.
    # ```ballerina
    # kafka:Error? result = consumer->close();
    # ```
    # + duration - Timeout duration (in seconds) for the close operation execution
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated remote function close(decimal duration = -1) returns Error?;

    # Commits the currently consumed offsets of the consumer.
    # ```ballerina
    # kafka:Error? result = consumer->commit();
    # ```
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated remote function 'commit() returns Error?;

    # Commits the given offsets of the specific topic partitions for the consumer.
    # ```ballerina
    # kafka:Error? result = consumer->commitOffset([partitionOffset1, partitionOffset2]);
    # ```
    # + offsets - Offsets to be committed
    # + duration - Timeout duration (in seconds) for the commit operation execution
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function commitOffset(PartitionOffset[] offsets, decimal duration = -1) returns Error?;

    # Retrieves the currently-assigned partitions of the consumer.
    # ```ballerina
    # kafka:TopicPartition[] result = check consumer->getAssignment();
    # ```
    # + return - Array of assigned partitions for the consumer if executes successfully or else a `kafka:Error`
    isolated remote function getAssignment() returns TopicPartition[]|Error;

    # Retrieves the available list of topics for a particular consumer.
    # ```ballerina
    # string[] result = check consumer->getAvailableTopics();
    # ```
    # + duration - Timeout duration (in seconds) for the execution of the `getAvailableTopics` operation
    # + return - Array of topics currently available (authorized) for the consumer to subscribe or else
    # a `kafka:Error`
    isolated remote function getAvailableTopics(decimal duration = -1) returns string[]|Error;

    # Retrieves the start offsets for a given set of partitions.
    # ```ballerina
    # kafka:PartitionOffset[] result = check consumer->getBeginningOffsets([topicPartition1, topicPartition2]);
    # ```
    # + partitions - Array of topic partitions to get the starting offsets
    # + duration - Timeout duration (in seconds) for the `getBeginningOffsets` execution
    # + return - Starting offsets for the given partitions if executes successfully or else a `kafka:Error`
    isolated remote function getBeginningOffsets(TopicPartition[] partitions, decimal duration = -1) returns PartitionOffset[]|Error;

    # Retrieves the lastly committed offset for the given topic partition.
    # ```ballerina
    # kafka:PartitionOffset? result = check consumer->getCommittedOffset(topicPartition);
    # ```
    # + partition - The `TopicPartition` in which the committed offset is returned to the consumer
    # + duration - Timeout duration (in seconds) for the `getCommittedOffset` operation to execute
    # + return - The last committed offset for a given partition for the consumer if there is a committed offset
    # present, `()` if there are no committed offsets, or else a `kafka:Error`
    isolated remote function getCommittedOffset(TopicPartition partition, decimal duration = -1) returns PartitionOffset|Error?;

    # Retrieves the last offsets for a given set of partitions.
    # ```ballerina
    # kafka:PartitionOffset[] result = check consumer->getEndOffsets([topicPartition1, topicPartition2]);
    # ```
    # + partitions - Set of partitions to get the last offsets
    # + duration - Timeout duration (in seconds) for the `getEndOffsets` operation to execute
    # + return - End offsets for the given partitions if executes successfully or else a `kafka:Error`
    isolated remote function getEndOffsets(TopicPartition[] partitions, decimal duration = -1) returns PartitionOffset[]|Error;

    # Retrieves the partitions, which are currently paused.
    # ```ballerina
    # kafka:TopicPartition[] result = check consumer->getPausedPartitions();
    # ```
    # + return - The set of partitions paused from message retrieval if executes successfully or else a `kafka:Error`
    isolated remote function getPausedPartitions() returns TopicPartition[]|Error;

    # Retrieves the offset of the next record that will be fetched if a record exists in that position.
    # ```ballerina
    # int result = check consumer->getPositionOffset(topicPartition);
    # ```
    # + partition - The `TopicPartition` in which the position is required
    # + duration - Timeout duration (in seconds) for the get position offset operation to execute
    # + return - Offset, which will be fetched next (if a record exists in that offset) or else a `kafka:Error` if
    # the operation fails
    isolated remote function getPositionOffset(TopicPartition partition, decimal duration = -1) returns int|Error;

    # Retrieves the set of topics, which are currently subscribed by the consumer.
    # ```ballerina
    # string[] result = check consumer->getSubscription();
    # ```
    # + return - Array of subscribed topics for the consumer if executes successfully or else a `kafka:Error`
    isolated remote function getSubscription() returns string[]|Error;

    # Retrieves the set of partitions to which the topic belongs.
    # ```ballerina
    # kafka:TopicPartition[] result = check consumer->getTopicPartitions("kafka-topic");
    # ```
    # + topic - The topic for which the partition information is needed
    # + duration - Timeout duration (in seconds) for the `getTopicPartitions` operation to execute
    # + return - Array of partitions for the given topic if executes successfully or else a `kafka:Error`
    isolated remote function getTopicPartitions(string topic, decimal duration = -1) returns TopicPartition[]|Error;

    # Pauses retrieving messages from a set of partitions.
    # ```ballerina
    # kafka:Error? result = consumer->pause([topicPartition1, topicPartition2]);
    # ```
    # + partitions - Set of topic partitions to pause the retrieval of messages
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function pause(TopicPartition[] partitions) returns Error?;

    # Polls the external broker to retrieve messages.
    # ```ballerina
    # kafka:AnydataConsumerRecord[] result = check consumer->poll(10);
    # ```
    # + timeout - Polling time in seconds
    # + T - Optional type description of the required data type
    # + return - Array of consumer records if executed successfully or else a `kafka:Error`
    isolated remote function poll(decimal timeout, typedesc<AnydataConsumerRecord[]> T = <>) returns T|Error;

    # Polls the external broker to retrieve messages in the required data type without the `kafka:AnydataConsumerRecord`
    # information.
    # ```ballerina
    # Person[] persons = check consumer->pollPayload(10);
    # ```
    # + timeout - Polling time in seconds
    # + T - Optional type description of the required data type
    # + return - Array of data in the required format if executed successfully or else a `kafka:Error`
    isolated remote function pollPayload(decimal timeout, typedesc<anydata[]> T = <>) returns T|Error;

    # Resumes retrieving messages from a set of partitions, which were paused earlier.
    # ```ballerina
    # kafka:Error? result = consumer->resume([topicPartition1, topicPartition2]);
    # ```
    # + partitions - Topic partitions to resume the retrieval of messages
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function resume(TopicPartition[] partitions) returns Error?;

    # Retrieves the offsets for the given topic partitions and timestamps.
    # ```ballerina
    # kafka:TopicPartitionOffset[] result = check consumer->offsetsForTimes([[topicPartition1, timestamp1], [topicPartition2, timestamp2]]);
    # ```
    # + topicPartitionTimestamps - Array of topic partitions with required timestamps
    # + duration - Timeout duration (in seconds) for the `offsetsForTimes` operation to execute
    # + return - Array of topic partition offsets if executed successfully or else a `kafka:Error`
    isolated remote function offsetsForTimes(TopicPartitionTimestamp[] topicPartitionTimestamps, decimal? duration = ()) returns TopicPartitionOffset[]|Error;

    # Seeks for a given offset in a topic partition.
    # ```ballerina
    # kafka:Error? result = consumer->seek(partitionOffset);
    # ```
    # + offset - The `PartitionOffset` to seek
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function seek(PartitionOffset offset) returns Error?;

    # Seeks to the beginning of the offsets for a given set of topic partitions.
    # ```ballerina
    # kafka:Error? result = consumer->seekToBeginning([topicPartition1, topicPartition2]);
    # ```
    # + partitions - The set of topic partitions to seek
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function seekToBeginning(TopicPartition[] partitions) returns Error?;

    # Seeks to the end of the offsets for a given set of topic partitions.
    # ```ballerina
    # kafka:Error? result = consumer->seekToEnd([topicPartition1, topicPartition2]);
    # ```
    # + partitions - The set of topic partitions to seek
    # + return - A `kafka:Error` if an error is encountered or else `()`
    isolated remote function seekToEnd(TopicPartition[] partitions) returns Error?;

    # Subscribes the consumer to the provided set of topics.
    # ```ballerina
    # kafka:Error? result = consumer->subscribe(["kafka-topic-1", "kafka-topic-2"]);
    # ```
    # + topics - The topic/array of topics to subscribe
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated remote function subscribe(string|string[] topics) returns Error?;

    # Subscribes the consumer to the topics, which match the provided pattern.
    # ```ballerina
    # kafka:Error? result = consumer->subscribeWithPattern("kafka.*");
    # ```
    # + regex - The pattern, which should be matched with the topics to be subscribed
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated remote function subscribeWithPattern(string regex) returns Error?;

    # Unsubscribes from all the topics that the consumer is subscribed to.
    # ```ballerina
    # kafka:Error? result = consumer->unsubscribe();
    # ```
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated remote function unsubscribe() returns Error?;
}

# Represents a Kafka producer endpoint.
public isolated client class Producer {
    # Creates a new `kafka:Producer`.
    # + bootstrapServers - List of remote server endpoints of Kafka brokers
    # + config - Configurations related to initializing a `kafka:Producer`
    # + return - A `kafka:Error` if closing the producer failed or else '()'
    isolated function init(string|string[] bootstrapServers, *ProducerConfiguration config) returns Error?;

    # Closes the producer connection to the external Kafka broker.
    # ```ballerina
    # kafka:Error? result = producer->close();
    # ```
    # + return - A `kafka:Error` if closing the producer failed or else '()'
    isolated remote function close() returns Error?;

    # Flushes the batch of records already sent to the broker by the producer.
    # ```ballerina
    # kafka:Error? result = producer->'flush();
    # ```
    # + return - A `kafka:Error` if records couldn't be flushed or else '()'
    isolated remote function 'flush() returns Error?;

    # Retrieves the topic partition information for the provided topic.
    # ```ballerina
    # kafka:TopicPartition[] result = check producer->getTopicPartitions("kafka-topic");
    # ```
    # + topic - The specific topic, of which the topic partition information is required
    # + return - A `kafka:TopicPartition` array for the given topic or else a `kafka:Error` if the operation fails
    isolated remote function getTopicPartitions(string topic) returns TopicPartition[]|Error;

    # Produces records to the Kafka server.
    # ```ballerina
    # kafka:Error? result = producer->send({value: "Hello World".toBytes(), topic: "kafka-topic"});
    # ```
    # + producerRecord - Record to be produced
    # + return - A `kafka:Error` if send action fails to send data or else '()'
    isolated remote function send(AnydataProducerRecord producerRecord) returns Error?;

    # Produces the records to the Kafka server and returns the relevant metadata.
    # ```ballerina
    # kafka:RecordMetadata metadata = check producer->sendWithMetadata({topic: "kafka-topic", value: "Hello World".toBytes()});
    # ```
    # + producerRecord - Record to be produced
    # + return - A `kafka:RecordMetadata` containing the metadata of the produced record if send action succeeds or
    # else a `kafka:Error`
    isolated remote function sendWithMetadata(AnydataProducerRecord producerRecord) returns RecordMetadata|Error;
}

// --- Listeners ---

# Represents a Kafka consumer endpoint.
public isolated class Listener {
    # Creates a new `kafka:Listener`.
    # + bootstrapServers - List of remote server endpoints of Kafka brokers
    # + config - Configurations related to the consumer endpoint
    # + return - A `kafka:Error` if an error is encountered or else '()'
    isolated function init(string|string[] bootstrapServers, *ConsumerConfiguration config) returns Error?;

    # Starts the registered services.
    # ```ballerina
    # error? result = listener.'start();
    # ```
    # + return - A `kafka:Error` if an error is encountered while starting the server or else `()`
    isolated function 'start() returns error?;

    # Stops the Kafka listener gracefully.
    # ```ballerina
    # error? result = listener.gracefulStop();
    # ```
    # + return - A `kafka:Error` if an error is encountered during the listener-stopping process or else `()`
    isolated function gracefulStop() returns error?;

    # Stops the kafka listener immediately.
    # ```ballerina
    # error? result = listener.immediateStop();
    # ```
    # + return - A `kafka:Error` if an error is encountered during the listener-stopping process or else `()`
    isolated function immediateStop() returns error?;

    # Attaches a service to the listener.
    # ```ballerina
    # error? result = listener.attach(kafkaService);
    # ```
    # + 'service - The service to be attached
    # + name - Name of the service
    # + return - A `kafka:Error` if an error is encountered while attaching the service or else `()`
    isolated function attach(Service 'service, string[]|string? name = ()) returns error?;

    # Detaches a consumer service from the listener.
    # ```ballerina
    # error? result = listener.detach(kafkaService);
    # ```
    # + 'service - The service to be detached
    # + return - A `kafka:Error` if an error is encountered while detaching a service or else `()`
    isolated function detach(Service 'service) returns error?;
}

// --- Service ---

service kafka:Service on new kafka:Listener(bootstrapServers, config) {
    // Central publishes no method contract for this service type. The listener may still require
    // one — add the resource or remote methods the package's guide shows; `bal library overview`
    // reproduces it.
}

// --- Annotations ---

# The annotation which is used to define the payload parameter in the `onConsumerRecord` service method.
public annotation KafkaPayload Payload on parameter;
