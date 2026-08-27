# Ballerina Dockerfile

Only when the user asks for a container image. Use this multi-stage build exactly as
written: the first stage compiles with the Ballerina toolchain, the second ships only a
JRE and the jar.

```dockerfile
FROM ballerina/ballerina:2201.13.5 AS builder
WORKDIR /src
COPY --chown=ballerina:troupe . .
RUN bal build && mv target/bin/*.jar /tmp/service.jar

FROM eclipse-temurin:21-jre
WORKDIR /app
COPY --from=builder /tmp/service.jar /app/service.jar
EXPOSE 9090
ENTRYPOINT ["java", "-jar", "/app/service.jar"]
```
