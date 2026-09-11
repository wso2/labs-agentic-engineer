# Ballerina from an OpenAPI Contract

Generating a service or a client from an OpenAPI contract. Generated code follows the
same rules as anything hand-written ([code-rules.md](code-rules.md)), and where its files
belong is [project-structure.md](project-structure.md)'s rule.

```bash
bal tool pull openapi                          # once per environment
bal openapi -i ./path/openapi.yaml --mode service --single-file                 # the spec this component implements
mkdir -p modules/weather && bal openapi -i ./path/weather_oas.yaml --mode client --single-file -o modules/weather   # each spec it calls
```

`--single-file` keeps each generated artefact in one file — types and utilities included.

After `--mode service`:

- The stub is the starting point: fill every empty resource body with Edit — an unfilled body is a compile error.
- Change the generated `new (9090, config = {host: "localhost"})` to `new (9090)` — localhost binding leaves the deployed container unreachable while it looks healthy.
- Delete the `bal new` scaffold's `main.bal` once the service exists.

After `--mode client`:

- **One client, one module**: `modules/weather` is reached as `import <package>.weather;`
  and `weather:Client`. `<package>` is `Ballerina.toml`'s `name`.
- The generated client and its types ARE the integration: call it, and do not re-declare
  its request or response records by hand.
- Handle the error responses the spec declares. A non-2xx the spec names is a case to
  answer, not an error to pass on to your own caller unchanged.
- Regenerate when the spec changes rather than editing generated files.
