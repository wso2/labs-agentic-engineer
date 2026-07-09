# thunder-app-operator Makefile
#
# The root Makefile builds/tests/lints this module directly (`go build`,
# `go test`, `go vet`, `golangci-lint`) since it's a workspace member listed
# in go.work — see the repo root Makefile. This Makefile only owns the one
# verb the root doesn't: codegen.
#
#   make generate   regenerate the CRD manifest from the +kubebuilder markers
#                   in api/v1alpha1. DeepCopy methods are hand-written in
#                   api/v1alpha1/deepcopy.go (all fields are scalar), not
#                   generated — see that file's doc comment.

CONTROLLER_TOOLS_VERSION := v0.21.0
CONTROLLER_GEN := go run sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: generate

generate:
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=config/crd
