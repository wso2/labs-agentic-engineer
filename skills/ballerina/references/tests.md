# Ballerina Tests

Only write tests if the user explicitly asks. Everything below applies once they do.
Test sources live in the package's `tests/` directory and follow the same rules as
production code ([code-rules.md](code-rules.md)).

- Use the `ballerina/test` module and any service-specific test libraries.
- Follow the `instructions` field in `ballerina/test` library docs when writing tests.
- Test an HTTP service through an `http:Client` against the running service — assert its public contract, not internals.
- Override `configurable` values for tests in `tests/Config.toml` (not the package's `Config.toml`, and never read either — see [project-structure.md](project-structure.md)).
- To mock a client or connector, wrap its construction in a small init function so `@test:Mock` can replace it.
- Use `dependsOn` only when test ordering is the behavior under test — not to sequence otherwise-independent tests.
- Run them with `bal test`; a test run compiles the package, so fix compilation errors the same way a `bal build` failure is fixed.
