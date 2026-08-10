<!-- bal-library overview v1 -->
# ballerinax/kafka 0.0.0-fixture

| | |
|---|---|
| Source | central |
| Clients | `Caller`, `Consumer`, `Producer` |
| Module functions | none |
| Errors | 3, listed below |
| Types | 56 declarations (14 records, 6 unions, 36 other), not listed here — read one with `type` |

## Next

- `bal-library ops ballerinax/kafka <path>` — navigate a client's operations
- `bal-library type ballerinax/kafka <Name> [--deps]` — read a declaration whole
- `bal-library api ballerinax/kafka` — every declaration, when nothing above answered

## Client `Caller`

Represents a Kafka caller, which can be used to commit the offsets consumed by the service.

### Remote functions — 3, call with `->`

```ballerina
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
```

## Client `Consumer`

Represents a Kafka consumer endpoint.

### Constructor

```ballerina
function init(string|string[] bootstrapServers, ConsumerConfiguration config) returns Error?;
```

### Remote functions — 24, call with `->`

```ballerina
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
```

## Client `Producer`

Represents a Kafka producer endpoint.

### Constructor

```ballerina
function init(string|string[] bootstrapServers, ProducerConfiguration config) returns Error?;
```

### Remote functions — 5, call with `->`

```ballerina
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
```

## Errors — 3

The subtype chain is what `is` tests against, so `e is <Name>` works off these lines directly.

```ballerina
# Defines the common error type for the module.
type Error distinct error;

# Represents an error, which occurred due to payload binding.
type PayloadBindingError distinct (Error&error);

# Represents an error, which occurred due to payload constraint validation.
type PayloadValidationError distinct (PayloadBindingError&error);
```

## Guide

*The package's own readme, verbatim, with its headings demoted two levels.*

#### Overview

Apache Kafka is a distributed event streaming platform used for high-performance data pipelines, streaming analytics, data integration, and mission-critical applications. The Kafka connector allows you to integrate with Kafka, providing the ability to produce and consume events from Kafka topics. It supports high-throughput, fault-tolerant, and scalable messaging, making it ideal for building real-time data processing systems.

##### Key Features

- Produce and consume messages from Kafka topics
- Support for consumer groups for scalable message processing
- Secure communication with SASL and SSL
- Comprehensive support for Kafka transactions
- Custom serialization and deserialization of messages
- GraalVM compatible for native image builds

##### Consumer and producer

###### Kafka producer

A Kafka producer is a Kafka client that publishes records to the Kafka cluster. The producer is thread-safe and sharing a single producer instance across threads will generally be faster than having multiple instances. When working with a Kafka producer, the first thing to do is to initialize the producer.
For the producer to execute successfully, an active Kafka broker should be available.

The code snippet given below initializes a producer with the basic configuration.

```ballerina
import ballerinax/kafka;

kafka:ProducerConfiguration producerConfiguration = {
    clientId: "basic-producer",
    acks: "all",
    retryCount: 3
};

kafka:Producer kafkaProducer = check new (kafka:DEFAULT_URL, producerConfiguration);
```

###### Kafka consumer

A Kafka consumer is a subscriber responsible for reading records from one or more topics and one or more partitions of a topic. When working with a Kafka consumer, the first thing to do is initialize the consumer.
For the consumer to execute successfully, an active Kafka broker should be available.

The code snippet given below initializes a consumer with the basic configuration.

```ballerina
kafka:ConsumerConfiguration consumerConfiguration = {
    groupId: "group-id",    // Unique string that identifies the consumer
    offsetReset: "earliest",    // Offset reset strategy if no initial offset
    topics: ["kafka-topic"]
};

kafka:Consumer kafkaConsumer = check new (kafka:DEFAULT_URL, consumerConfiguration);
```

##### Listener
The Kafka consumer can be used as a listener to a set of topics without the need to manually `poll` the messages.

You can use the `Caller` to manually commit the offsets of the messages that are read by the service. The following code snippet shows how to initialize and define the listener and how to commit the offsets manually.

```ballerina
kafka:ConsumerConfiguration consumerConfiguration = {
    groupId: "group-id",
    topics: ["kafka-topic-1"],
    pollingInterval: 1,
    autoCommit: false
};

listener kafka:Listener kafkaListener = new (kafka:DEFAULT_URL, consumerConfiguration);

service on kafkaListener {
    remote function onConsumerRecord(kafka:Caller caller, kafka:BytesConsumerRecord[] records) {
        // processes the records
        ...
        // commits the offsets manually
        kafka:Error? commitResult = caller->commit();

        if commitResult is kafka:Error {
            log:printError("Error occurred while committing the offsets for the consumer ", 'error = commitResult);
        }
    }
}
```

##### Data serialization

Serialization is the process of converting data into a stream of bytes that is used for transmission. Kafka
stores and transmits these bytes of arrays in its queue. Deserialization does the opposite of serialization
in which bytes of arrays are converted into the desired data type.

Currently, this package only supports the `byte array` data type for both the keys and values. The following code snippets
show how to produce and read a message from Kafka.

```ballerina
string message = "Hello World, Ballerina";
string key = "my-key";
// converts the message and key to a byte array
check kafkaProducer->send({ topic: "test-kafka-topic", key: key.toBytes(), value: message.toBytes() });
```

```ballerina
kafka:BytesConsumerRecord[] records = check kafkaConsumer->poll(1);

foreach var kafkaRecord in records {
    byte[] messageContent = kafkaRecord.value;
    // tries to generate the string value from the byte array
    string result = check string:fromBytes(messageContent);
    io:println("The result is : ", result);
}
```

##### Concurrency

In Kafka, records are grouped into smaller units called partitions. These can be processed independently without
compromising the correctness of the results and lays the foundation for parallel processing. This can be achieved by
using multiple consumers within the same group each reading and processing data from a subset of topic partitions and
running in a single thread.

Topic partitions are assigned to consumers automatically or you can manually assign topic partitions.

The following code snippet joins a consumer to the `consumer-group` and assigns it to a topic partition manually.

```ballerina
kafka:ConsumerConfiguration consumerConfiguration = {
    // `groupId` determines the consumer group
    groupId: "consumer-group",
    pollingInterval: 1,
    autoCommit: false
};

kafka:Consumer kafkaConsumer = check new (kafka:DEFAULT_URL, consumerConfiguration);
// creates a topic partition
kafka:TopicPartition topicPartition = {
    topic: "kafka-topic-1",
    partition: 1
};
// passes the topic partitions to the assign function as an array
check kafkaConsumer->assign([topicPartition]);
```

#### Examples

The following example shows how to use the Ballerina `kafka` connector to produce and consume messages using an Apache Kafka message broker.

- [**Order manager**](https://github.com/ballerina-platform/module-ballerinax-kafka/tree/master/examples/order-manager): A simple order management system that uses Kafka to process orders.
- [**Word count calculator**](https://github.com/ballerina-platform/module-ballerinax-kafka/tree/master/examples/secured-word-count-calculator): A word count calculator that reads messages from a Kafka topic and counts the occurrences of each word.
- [**Twitter filter**](https://github.com/ballerina-platform/module-ballerinax-kafka/tree/master/examples/twitter-filter): A Twitter filter that reads tweets from a Kafka topic and filters them based on certain criteria.
- [**Stock trading analyzer**](https://github.com/ballerina-platform/module-ballerinax-kafka/tree/master/examples/stock-trading-analyzer): This example demonstrates a simulated stock trading system built using Kafka and Ballerina.
- [**Banking transaction processor**](https://github.com/ballerina-platform/module-ballerinax-kafka/tree/master/examples/banking-transaction-system): A banking transaction processor that processes banking transactions using Kafka. It illustrates how banking transactions can be published and consumed in real time, while also integrating with Confluent Schema Registry to manage message schemas between the producer and consumer.


##### Report issues

To report bugs, request new features, start new discussions, view project boards, etc., go to the [Ballerina standard library parent repository](https://github.com/ballerina-platform/ballerina-standard-library).

##### Useful links

- Chat live with us via our [Discord server](https://discord.gg/ballerinalang).
- Post all technical questions on Stack Overflow with the [#ballerina](https://stackoverflow.com/questions/tagged/ballerina) tag.
