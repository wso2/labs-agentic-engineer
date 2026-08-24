# OpenChoreo resource binding behavior pinned by AEP

AEP's external dependency authoring relies on OpenChoreo **v1.1.1**, the version
set in `deployments/scripts/env.sh`. Re-check these facts when that pin changes.

## Environment configs

The resource pipeline builds its CEL context from
`ResourceReleaseBinding.spec.resourceTypeEnvironmentConfigs`, prunes it against
the ResourceType schema, and applies schema defaults before rendering
([pipeline.go lines 294–317](https://github.com/openchoreo/openchoreo/blob/v1.1.1/internal/pipeline/resource/pipeline.go#L294-L317)).
The template engine preserves an explicitly supplied empty string, so a present
key renders as `""`. By contrast, accessing an absent map key is a runtime error
(`no such key`), as pinned by the upstream test
([engine_test.go lines 599–604](https://github.com/openchoreo/openchoreo/blob/v1.1.1/internal/template/engine_test.go#L599-L604)); manifest rendering returns that
CEL error instead of a partial binding render
([pipeline.go lines 66–89](https://github.com/openchoreo/openchoreo/blob/v1.1.1/internal/pipeline/resource/pipeline.go#L66-L89)).

Consequently AEP must author every design-declared key. Plain keys without a
default and `secretStorePath` are present with empty values; declared plain
defaults are seeded.

## Ready is not configured

The binding controller gives an entry's explicit `readyWhen` precedence over
kind health and accepts it when it evaluates true
([controller_status.go lines 174–199](https://github.com/openchoreo/openchoreo/blob/v1.1.1/internal/controller/resourcereleasebinding/controller_status.go#L174-L199)).
AEP's external ResourceType deliberately authors `${true}` for its
`ExternalSecret`. Without that override, OpenChoreo classifies unknown kinds via
`getUnknownResourceHealth`, which reports an existing unknown resource healthy
([controller_status.go lines 171–184](https://github.com/openchoreo/openchoreo/blob/v1.1.1/internal/controller/renderedrelease/controller_status.go#L171-L184),
[lines 392–396](https://github.com/openchoreo/openchoreo/blob/v1.1.1/internal/controller/renderedrelease/controller_status.go#L392-L396)).

Therefore a binding can be Ready while every external value is unset. AEP's
project readiness endpoint derives `configured` independently from the current
design union schema and binding values; it never gates on the Ready condition.
