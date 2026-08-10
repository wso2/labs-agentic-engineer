// ============================================================
// Library: ballerinax/kafka
// Apache Kafka is a distributed event streaming platform used for high-performance data pipelines, streaming analytics, data integration, and mission-critical applications. The Kafka connector allows you to integrate with Kafka, providing the ability to produce and consume events from Kafka topics. It supports high-throughput, fault-tolerant, and scalable messaging, making it ideal for building real-time data processing systems.
// ============================================================
import ballerinax/kafka;

// --- Types ---

# Type related to anydata consumer record.

type AnydataConsumerRecord record {
    # Key that is included in the record
    anydata key?;
    # Anydata record content
    anydata value;
    # Timestamp of the record, in milliseconds since epoch
    int timestamp;
    # Topic partition position in which the consumed record is stored
    PartitionOffset offset;
    # Map of headers included with the record
    map<byte[]|byte[]|string|string[]> headers;
};

# Details related to the anydata producer record.

type AnydataProducerRecord record {
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
    map<byte[]|byte[]|string|string[]> headers?;
};

# Configurations related to Kafka authentication mechanisms.

type AuthenticationConfiguration record {
    # Type of the authentication mechanism. Currently `SASL_PLAIN`, `SASL_SCRAM_256` & `SASL_SCRAM_512`
is supported
    AuthenticationMechanism mechanism = AUTH_SASL_PLAIN;
    # The username to authenticate the Kafka producer/consumer
    string username;
    # The password to authenticate the Kafka producer/consumer
    string password;
};

# Subtype related to `kafka:AnydataConsumerRecord` record.

type BytesConsumerRecord record {
    # Key that is included in the record
    anydata key;
    # Record content in bytes
    byte[] value;
    # Timestamp of the record, in milliseconds since epoch
    int timestamp;
    # Topic partition position in which the consumed record is stored
    PartitionOffset offset;
    # Headers as a byte[] or byte[][]
    map<byte[]|byte[]> headers;
};

# Subtype related to `kafka:AnydataProducerRecord` record.

type BytesProducerRecord record {
    # Topic to which the record will be appended
    string topic;
    # Key that is included in the record
    anydata key;
    # Record content in bytes
    byte[] value;
    # Timestamp of the record, in milliseconds since epoch
    int timestamp;
    # Partition to which the record should be sent
    int partition;
    # Headers as a byte[] or byte[][]
    map<byte[]|byte[]> headers?;
};

# Represents a combination of certificate, private key, and private key password if encrypted.

type CertKey record {
    # A file containing the certificate
    string certFile;
    # A file containing the private key in PKCS8 format
    string keyFile;
    # Password of the private key if it is encrypted
    string keyPassword?;
};

# Configurations related to consumer endpoint.

type ConsumerConfiguration record {
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
is used
    string schemaRegistryUrl?;
    # Configurations to initialize a schema registry
    map<anydata> schemaRegistryConfig?;
    # Key deserialization type
    DeserializerType keyDeserializerType = DES_BYTE_ARRAY;
    # Value deserialization type
    DeserializerType valueDeserializerType = DES_BYTE_ARRAY;
    # Additional properties for the property fields not provided by the Ballerina `kafka` module. Use
this with caution since this can override any of the fields. It is not recomendded to use
this field except in an extreme situation
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
to the messages. This may add some overhead and might need to be set to `false` if extreme
performance is required
    boolean checkCRCS = true;
    # Whether records from internal topics should be exposed to the consumer
    boolean excludeInternalTopics = true;
    # Decouples processing
    boolean decoupleProcessing = false;
    # Configuration related to constraint validation check
    boolean validation = true;
    # Automatically seeks past the erroneous records in the event of an data-binding or
validating constraints failure
    boolean autoSeekOnValidationFailure = true;
    # Configurations related to SSL/TLS encryption
    SecureSocket secureSocket?;
    # Authentication-related configurations for the `kafka:Consumer`
    AuthenticationConfiguration auth?;
    # Type of the security protocol to use in the broker connection
    SecurityProtocol securityProtocol = PROTOCOL_PLAINTEXT;
};

# Defines the Payload remote function parameter.

type KafkaPayload record {
};

# Represents an offset and a timestamp for a topic partition.

type OffsetAndTimestamp record {
    # The offset of the record in the topic partition
    int offset;
    # The timestamp of the record in the topic partition
    int timestamp;
    # The leader epoch of the record in the topic partition
    int? leaderEpoch = ();
};

# Represents the topic partition position in which the consumed record is stored.

type PartitionOffset record {
    # The `kafka:TopicPartition` to which the record is related
    TopicPartition partition;
    # Offset in which the record is stored in the partition
    int offset;
};

# Represents the `kafka:Producer` configuration.

type ProducerConfiguration record {
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
serializer is used
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
this with caution since this can override any of the fields. It is not recomendded to use
this field except in an extreme situation
    map<string> additionalProperties?;
    # Total bytes of memory the producer can use to buffer records
    int bufferMemory?;
    # Number of retries to resend a record
    int retryCount?;
    # Maximum number of bytes to be batched together when sending the records. Records exceeding this limit will
not be batched. Setting this to 0 will disable batching
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
};

# Represents metadata of a Kafka record.

type RecordMetadata record {
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
};

# Configurations for secure communication with the Kafka server.

type SecureSocket record {
    # Configurations associated with crypto:TrustStore or single certificate file that the client trusts
    crypto:TrustStore|string cert; // Special Agent Note: TrustStore FROM ballerina/crypto package
    # Configurations associated with crypto:KeyStore or combination of certificate and private key of the client
    record {crypto:KeyStore keyStore; string keyPassword; }|CertKey key?;
    # SSL/TLS protocol related options
    record {Protocol name; string[] versions; } protocol?;
    # List of ciphers to be used. By default, all the available cipher suites are supported
    string[] ciphers?;
    # Name of the security provider used for SSL connections. The default value is the default security provider
of the JVM
    string provider?;
};

# Represents a topic partition.

type TopicPartition record {
    # Topic to which the partition is related
    string topic;
    # Index of the specific partition
    int partition;
};

# Defines the common error type for the module.
type Error error;

# Represents an error, which occurred due to payload binding.
type PayloadBindingError error;

# Represents an error, which occurred due to payload constraint validation.
type PayloadValidationError error;

# Producer acknowledgement type is 'all'. This will guarantee that the record will not be lost as long as at least one
# in-sync replica is alive.
const string ACKS_ALL = ""all"";

# Producer acknowledgement type '0'. If the acknowledgement type set to this, the producer will not wait for any
# acknowledgement from the server.
const string ACKS_NONE = ""0"";

# Producer acknowledgement type '1'. If the acknowledgement type set to this, the leader will write the record to its
# A local log will respond without waiting FOR full acknowledgement from all the followers.
const string ACKS_SINGLE = ""1"";

# Kafka SASL_PLAIN authentication mechanism
const string AUTH_SASL_PLAIN = ""PLAIN"";

# Kafka SASL_SCRAM authentication mechanism
const string AUTH_SASL_SCRAM_SHA_256 = ""SCRAM-SHA-256"";

const string AUTH_SASL_SCRAM_SHA_512 = ""SCRAM-SHA-512"";

# Kafka GZIP compression type.
const string COMPRESSION_GZIP = ""gzip"";

# Kafka LZ4 compression type.
const string COMPRESSION_LZ4 = ""lz4"";

# No compression.
const string COMPRESSION_NONE = ""none"";

# Kafka Snappy compression type.
const string COMPRESSION_SNAPPY = ""snappy"";

# Kafka ZSTD compression type.
const string COMPRESSION_ZSTD = ""zstd"";

# The default server URL.
const string DEFAULT_URL = ""localhost:9092"";

# User-defined deserializer.
const string DES_CUSTOM = ""CUSTOM"";

# In-built Kafka float deserializer.
const string DES_FLOAT = ""FLOAT"";

# In-built Kafka int deserializer.
const string DES_INT = ""INT"";

# In-built Kafka byte array deserializer.
# In-built Kafka string deserializer.
const string DES_STRING = ""STRING"";

# Configures the consumer to read the committed messages only in the transactional mode when poll() is called.
const string ISOLATION_COMMITTED = ""read_committed"";

# Configures the consumer to read all the messages including the aborted ones.
const string ISOLATION_UNCOMMITTED = ""read_uncommitted"";

# Automatically reset the consumer offset to the earliest offset
const string OFFSET_RESET_EARLIEST = ""earliest"";

# Automatically reset the consumer offset to the latest offset
const string OFFSET_RESET_LATEST = ""latest"";

# If the `offsetReset` is set to `OFFSET_RESET_NONE`, the consumer will give an error if no previous offset is found
# for the consumer group
const string OFFSET_RESET_NONE = ""none"";

# Represents Kafka un-authenticated, non-encrypted channel
const string PROTOCOL_PLAINTEXT = ""PLAINTEXT"";

# Represents Kafka authenticated, non-encrypted channel
const string PROTOCOL_SASL_PLAINTEXT = ""SASL_PLAINTEXT"";

# Represents Kafka SASL authenticated, SSL channel
const string PROTOCOL_SASL_SSL = ""SASL_SSL"";

# Represents Kafka SSL channel
const string PROTOCOL_SSL = ""SSL"";

# User-defined serializer.
const string SER_CUSTOM = ""CUSTOM"";

# In-built Kafka float serializer.
const string SER_FLOAT = ""FLOAT"";

# In-built Kafka int serializer.
const string SER_INT = ""INT"";

# In-built Kafka string serializer.
const string SER_STRING = ""STRING"";

# Kafka in-built deserializer type.
enum DeserializerType {
    DES_BYTE_ARRAY,
    DES_AVRO
}

# Represents protocol options.
enum Protocol {
    SSL,
    TLS,
    DTLS
}

# Kafka in-built serializer types.
enum SerializerType {
    SER_BYTE_ARRAY,
    SER_AVRO
}

class AvroDeserializer {
}

class AvroSerializer {
}

class Deserializer {
}

class Serializer {
}

# Represents the different types of offset-reset methods of the Kafka consumer.
type OffsetResetMethod OFFSET_RESET_EARLIEST|OFFSET_RESET_LATEST|OFFSET_RESET_NONE;

# `kafka:Consumer` isolation level type.
type IsolationLevel ISOLATION_COMMITTED|ISOLATION_UNCOMMITTED;

# `kafka:Producer` acknowledgement types.
type ProducerAcks ACKS_ALL|ACKS_NONE|ACKS_SINGLE;

# Kafka compression types to compress the messages.
type CompressionType COMPRESSION_NONE|COMPRESSION_GZIP|COMPRESSION_SNAPPY|COMPRESSION_LZ4|COMPRESSION_ZSTD;

# Represents the supported Kafka SASL authentication mechanisms.
type AuthenticationMechanism AUTH_SASL_PLAIN|AUTH_SASL_SCRAM_SHA_256|AUTH_SASL_SCRAM_SHA_512;

# Represents the supported security protocols for Kafka clients.
type SecurityProtocol PROTOCOL_PLAINTEXT|PROTOCOL_SASL_PLAINTEXT|PROTOCOL_SASL_SSL|PROTOCOL_SSL;

// --- Client ---

# Represents a Kafka caller, which can be used to commit the offsets consumed by the service.
client class Caller {

    # Commits the currently consumed offsets of the service.
    # ```ballerina
    # kafka:Error? result = caller->commit();
    # ```
    remote function 'commit() returns Error?;

    # Commits the given offsets and partitions for the given topics of the service.
    # ```ballerina
    # kafka:Error? result = caller->commitOffset([partitionOffset1, partitionOffset2]);
    # ```
    remote function commitOffset(PartitionOffset[] offsets, decimal duration = -1) returns Error?;

    # Seeks for a given offset in a topic partition.
    # ```ballerina
    # kafka:Error? result = consumer->seek(partitionOffset);
    # ```
    remote function seek(PartitionOffset offset) returns Error?;
}

# Represents a Kafka consumer endpoint.
client class Consumer {
    function init(string|string[] bootstrapServers, ConsumerConfiguration config) returns Error?;

    # Assigns consumer to a set of topic partitions.
    # ```ballerina
    # kafka:Error? result = consumer->assign([topicPartition1, topicPartition2]);
    # ```
    remote function assign(TopicPartition[] partitions) returns Error?;

    # Closes the consumer connection with the external Kafka broker.
    # ```ballerina
    # kafka:Error? result = consumer->close();
    # ```
    remote function close(decimal duration = -1) returns Error?;

    # Commits the currently consumed offsets of the consumer.
    # ```ballerina
    # kafka:Error? result = consumer->commit();
    # ```
    remote function 'commit() returns Error?;

    # Commits the given offsets of the specific topic partitions for the consumer.
    # ```ballerina
    # kafka:Error? result = consumer->commitOffset([partitionOffset1, partitionOffset2]);
    # ```
    remote function commitOffset(PartitionOffset[] offsets, decimal duration = -1) returns Error?;

    # Retrieves the currently-assigned partitions of the consumer.
    # ```ballerina
    # kafka:TopicPartition[] result = check consumer->getAssignment();
    # ```
    remote function getAssignment() returns TopicPartition[]|Error;

    # Retrieves the available list of topics for a particular consumer.
    # ```ballerina
    # string[] result = check consumer->getAvailableTopics();
    # ```
    remote function getAvailableTopics(decimal duration = -1) returns string[]|Error;

    # Retrieves the start offsets for a given set of partitions.
    # ```ballerina
    # kafka:PartitionOffset[] result = check consumer->getBeginningOffsets([topicPartition1, topicPartition2]);
    # ```
    remote function getBeginningOffsets(TopicPartition[] partitions, decimal duration = -1) returns PartitionOffset[]|Error;

    # Retrieves the lastly committed offset for the given topic partition.
    # ```ballerina
    # kafka:PartitionOffset? result = check consumer->getCommittedOffset(topicPartition);
    # ```
    remote function getCommittedOffset(TopicPartition partition, decimal duration = -1) returns PartitionOffset|Error?;

    # Retrieves the last offsets for a given set of partitions.
    # ```ballerina
    # kafka:PartitionOffset[] result = check consumer->getEndOffsets([topicPartition1, topicPartition2]);
    # ```
    remote function getEndOffsets(TopicPartition[] partitions, decimal duration = -1) returns PartitionOffset[]|Error;

    # Retrieves the partitions, which are currently paused.
    # ```ballerina
    # kafka:TopicPartition[] result = check consumer->getPausedPartitions();
    # ```
    remote function getPausedPartitions() returns TopicPartition[]|Error;

    # Retrieves the offset of the next record that will be fetched if a record exists in that position.
    # ```ballerina
    # int result = check consumer->getPositionOffset(topicPartition);
    # ```
    remote function getPositionOffset(TopicPartition partition, decimal duration = -1) returns int|Error;

    # Retrieves the set of topics, which are currently subscribed by the consumer.
    # ```ballerina
    # string[] result = check consumer->getSubscription();
    # ```
    remote function getSubscription() returns string[]|Error;

    # Retrieves the set of partitions to which the topic belongs.
    # ```ballerina
    # kafka:TopicPartition[] result = check consumer->getTopicPartitions("kafka-topic");
    # ```
    remote function getTopicPartitions(string topic, decimal duration = -1) returns TopicPartition[]|Error;

    # Pauses retrieving messages from a set of partitions.
    # ```ballerina
    # kafka:Error? result = consumer->pause([topicPartition1, topicPartition2]);
    # ```
    remote function pause(TopicPartition[] partitions) returns Error?;

    # Polls the external broker to retrieve messages.
    # ```ballerina
    # kafka:AnydataConsumerRecord[] result = check consumer->poll(10);
    # ```
    remote function poll(decimal timeout, typedesc<AnydataConsumerRecord[]> T = <>) returns T|Error;

    # Polls the external broker to retrieve messages in the required data type without the `kafka:AnydataConsumerRecord`
    # information.
    # ```ballerina
    # Person[] persons = check consumer->pollPayload(10);
    # ```
    remote function pollPayload(decimal timeout, typedesc<anydata[]> T = <>) returns T|Error;

    # Resumes retrieving messages from a set of partitions, which were paused earlier.
    # ```ballerina
    # kafka:Error? result = consumer->resume([topicPartition1, topicPartition2]);
    # ```
    remote function resume(TopicPartition[] partitions) returns Error?;

    # Retrieves the offsets for the given topic partitions and timestamps.
    # ```ballerina
    # kafka:TopicPartitionOffset[] result = check consumer->offsetsForTimes([[topicPartition1, timestamp1], [topicPartition2, timestamp2]]);
    # ```
    remote function offsetsForTimes(TopicPartitionTimestamp[] topicPartitionTimestamps, decimal? duration = ()) returns TopicPartitionOffset[]|Error;

    # Seeks for a given offset in a topic partition.
    # ```ballerina
    # kafka:Error? result = consumer->seek(partitionOffset);
    # ```
    remote function seek(PartitionOffset offset) returns Error?;

    # Seeks to the beginning of the offsets for a given set of topic partitions.
    # ```ballerina
    # kafka:Error? result = consumer->seekToBeginning([topicPartition1, topicPartition2]);
    # ```
    remote function seekToBeginning(TopicPartition[] partitions) returns Error?;

    # Seeks to the end of the offsets for a given set of topic partitions.
    # ```ballerina
    # kafka:Error? result = consumer->seekToEnd([topicPartition1, topicPartition2]);
    # ```
    remote function seekToEnd(TopicPartition[] partitions) returns Error?;

    # Subscribes the consumer to the provided set of topics.
    # ```ballerina
    # kafka:Error? result = consumer->subscribe(["kafka-topic-1", "kafka-topic-2"]);
    # ```
    remote function subscribe(string|string[] topics) returns Error?;

    # Subscribes the consumer to the topics, which match the provided pattern.
    # ```ballerina
    # kafka:Error? result = consumer->subscribeWithPattern("kafka.*");
    # ```
    remote function subscribeWithPattern(string regex) returns Error?;

    # Unsubscribes from all the topics that the consumer is subscribed to.
    # ```ballerina
    # kafka:Error? result = consumer->unsubscribe();
    # ```
    remote function unsubscribe() returns Error?;
}

# Represents a Kafka producer endpoint.
client class Producer {
    function init(string|string[] bootstrapServers, ProducerConfiguration config) returns Error?;

    # Closes the producer connection to the external Kafka broker.
    # ```ballerina
    # kafka:Error? result = producer->close();
    # ```
    remote function close() returns Error?;

    # Flushes the batch of records already sent to the broker by the producer.
    # ```ballerina
    # kafka:Error? result = producer->'flush();
    # ```
    remote function 'flush() returns Error?;

    # Retrieves the topic partition information for the provided topic.
    # ```ballerina
    # kafka:TopicPartition[] result = check producer->getTopicPartitions("kafka-topic");
    # ```
    remote function getTopicPartitions(string topic) returns TopicPartition[]|Error;

    # Produces records to the Kafka server.
    # ```ballerina
    # kafka:Error? result = producer->send({value: "Hello World".toBytes(), topic: "kafka-topic"});
    # ```
    remote function send(AnydataProducerRecord producerRecord) returns Error?;

    # Produces the records to the Kafka server and returns the relevant metadata.
    # ```ballerina
    # kafka:RecordMetadata metadata = check producer->sendWithMetadata({topic: "kafka-topic", value: "Hello World".toBytes()});
    # ```
    remote function sendWithMetadata(AnydataProducerRecord producerRecord) returns RecordMetadata|Error;
}

// --- Service ---

service kafka:Service on new kafka:Listener(string|string[] bootstrapServers, ConsumerConfiguration config) {
}
