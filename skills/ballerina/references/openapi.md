# Ballerina from an OpenAPI Contract

Generating a service or a client from an OpenAPI contract. Generated code follows the
same rules as anything hand-written ([code-rules.md](code-rules.md)), and where its files
belong is [project-structure.md](project-structure.md)'s rule.

```bash
bal tool pull openapi                          # once per environment
bal openapi -i oas.yaml --mode service         # generate service from openapi spec
bal openapi -i oas.yaml --mode client          # generate client from openapi spec
```

After `--mode service`:

- The stub is the starting point: fill every empty resource body with Edit — an unfilled body is a compile error.
- Change the generated `new (9090, config = {host: "localhost"})` to `new (9090)` — localhost binding leaves the deployed container unreachable while it looks healthy.
- Delete the `bal new` scaffold's `main.bal` once the service exists.
