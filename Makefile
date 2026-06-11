# License header management.
#
# Headers follow the WSO2 Apache-2.0 template in .github/license-header.txt and
# are applied with google/addlicense, which picks the correct comment style per
# file type (// for Go, /** */ for TS/TSX, # for shell + Dockerfile) and is
# idempotent (already-headered files are skipped).
#
#   make license        add headers to all in-scope source files
#   make license-check  fail if any in-scope source file is missing a header
#
# Go header enforcement is also wired into golangci-lint via the goheader linter
# (see asdlc-service/.golangci.yml and database-service/.golangci.yml).

ADDLICENSE := go run github.com/google/addlicense@v1.2.0
LICENSE_HEADER := .github/license-header.txt

# In-scope source files: Go, TS/TSX, shell, Dockerfile.
# Excludes generated Go (*.gen.go), mocks, and any vendored/build output.
LICENSE_FILES = $(shell git ls-files | \
	grep -E '\.(go|ts|tsx|sh)$$|(^|/)Dockerfile$$' | \
	grep -vE '\.gen\.go$$|_mock\.go$$|/mocks/|/node_modules/|/dist/|/build/')

.PHONY: license license-check

license:
	$(ADDLICENSE) -f $(LICENSE_HEADER) $(LICENSE_FILES)

license-check:
	$(ADDLICENSE) -check -f $(LICENSE_HEADER) $(LICENSE_FILES)
