<!-- bal library overview v1 -->
# ballerinax/kafka 0.0.0-fixture

| | |
|---|---|
| Clients | 3 — `Caller`, `Consumer`, `Producer` — `bal library client ballerinax/kafka` |
| Classes | 6 — `AvroDeserializer`, `AvroSerializer`, `Deserializer`, `Serializer`, `Service`, `Listener` — `bal library class ballerinax/kafka` |
| Module functions | none |
| Errors | 3 — `Error`, `PayloadBindingError`, `PayloadValidationError` — read one with `bal library type ballerinax/kafka <Name>` |
| Types | 60 declarations (14 records, 8 type aliases, 3 enums, 29 constants, 6 classes and object types), not listed here — read one with `type` |
| Guide | 155 lines — `bal library guide ballerinax/kafka` |

Guide chunks (5): 1. `Kafka producer`  2. `Kafka consumer`  3. `Listener`  4. `Data serialization`  5. `Concurrency` — `bal library guide ballerinax/kafka <n>`

## Next

- `bal library client ballerinax/kafka` — 3 clients, called with `->`
- `bal library class ballerinax/kafka` — 6 classes and object types, called with `.`
- `bal library overview ballerinax/kafka -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/kafka <Name> [-r]` — a declaration whole, with the types it names

## Clients — 3

- `Caller` — 3 remote · `bal library client ballerinax/kafka Caller`
- `Consumer` — 24 remote · `bal library client ballerinax/kafka Consumer`
- `Producer` — 5 remote · `bal library client ballerinax/kafka Producer`

## Classes and object types — 6

- `AvroDeserializer` — 1 normal · `bal library class ballerinax/kafka AvroDeserializer`
- `AvroSerializer` — 1 normal · `bal library class ballerinax/kafka AvroSerializer`
- `Deserializer` — 1 normal · `bal library class ballerinax/kafka Deserializer`
- `Serializer` — 1 normal · `bal library class ballerinax/kafka Serializer`
- `Service` — nothing callable · `bal library class ballerinax/kafka Service`
- `Listener` — 5 normal · `bal library class ballerinax/kafka Listener`

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/kafka readme usage -->

```ballerina
import ballerinax/kafka;

kafka:ProducerConfiguration producerConfiguration = {
    clientId: "basic-producer",
    acks: "all",
    retryCount: 3
};

kafka:Producer kafkaProducer = check new (kafka:DEFAULT_URL, producerConfiguration);
```

```ballerina
kafka:ConsumerConfiguration consumerConfiguration = {
    groupId: "group-id",    // Unique string that identifies the consumer
    offsetReset: "earliest",    // Offset reset strategy if no initial offset
    topics: ["kafka-topic"]
};

kafka:Consumer kafkaConsumer = check new (kafka:DEFAULT_URL, consumerConfiguration);
```

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

<!-- guide: end ballerinax/kafka readme usage -->
