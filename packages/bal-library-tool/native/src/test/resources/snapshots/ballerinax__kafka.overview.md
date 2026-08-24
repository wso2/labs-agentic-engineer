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

## Quickstart

*Quoted from the package's own readme and checked against this version's declarations. A line marked `⚠` names something this version does not declare. The signatures the container verbs generate win wherever the two disagree.*

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
string message = "Hello World, Ballerina";
string key = "my-key";
// converts the message and key to a byte array
check kafkaProducer->send({ topic: "test-kafka-topic", key: key.toBytes(), value: message.toBytes() });
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

<!-- guide: end ballerinax/kafka readme usage -->

3 more examples — `bal library guide ballerinax/kafka`

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
