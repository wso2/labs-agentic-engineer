# Ballerina Library Tool

Reads a Ballerina package off Ballerina Central and answers five addressed questions about it. It enables
AI copilots and developers to discover packages and read their real signatures — clients, resource
functions, type declarations and error hierarchies — instead of guessing them.

## Usage

```bash
bal library search   <keywords...>                           # find a package by keyword
bal library overview <org/name> [version] [--client C]       # readme + every signature + errors
bal library ops      <org/name> [path] [--client C] [--sigs]  # a client's operations, by path
bal library type     <org/name> <Name>... [--deps]           # one declaration, as Ballerina
bal library api      <org/name>                              # every declaration, in one document
```

```bash
bal library search kafka messaging
bal library overview ballerinax/kafka
bal library ops ballerinax/github repos
bal library ops ballerinax/github 'repos/*/*' --sigs
bal library type ballerina/http ClientRequestError --deps
```

Run `bal library --help` for the flags, the exit-code contract and where the cache landed.
