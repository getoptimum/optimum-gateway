# Recipes rely on `set -o pipefail` (e.g. license-check); force bash since the
# default /bin/sh is dash on Debian/Ubuntu and does not support it.
SHELL := /bin/bash
COVERAGE_THRESHOLD:=70
DOCKER_HUB_USER:=getoptimum
COVERAGE_TOTAL := $(shell go tool cover -func=cover.out | grep total | grep -Eo '[0-9]+\.[0-9]+')
COVERAGE_PASS_THRESHOLD := $(shell echo "$(COVERAGE_TOTAL) $(COVERAGE_THRESHOLD)" | awk '{print ($$1 >= $$2)}')
# Known vulns with no release-level fix or temporary accepted risk (ignored by govulncheck filter)
# GO-2024-3218: reachable (gateway runs a permissionless kad-dht for mump2p discovery),
# no patched version exists. The acceptance is currently incomplete: default-deny mesh
# admission is gone with the v1 engine and its replacement is not wired yet. Rationale
# and the merge blocker are in govulncheck.yaml and #924.
VULN_EXCEPTION_NAMES := ["GO-2024-3218"]

help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

vulcheck: ## Runs vulnerability check
	$(info Running vulnerability check...)
	@set -eu; \
	govulncheck --format openvex ./... | \
	jq --argjson exclude_list '$(VULN_EXCEPTION_NAMES)' \
		'[.statements[] | select(.status=="affected" and (.vulnerability.name | IN($$exclude_list[]) | not))]' > vulns.json; \
	if [ "$$(jq 'length' vulns.json)" -gt 0 ]; then \
		echo "Vulnerabilities found:"; \
		jq . vulns.json; rm vulns.json; exit 1; \
	else \
		rm vulns.json; \
	fi

build_hermes_image: ## Build Hermes docker image
	docker login
	cd /tmp && git clone git@github.com:probe-lab/hermes.git && cd /tmp/hermes && docker build -t ${DOCKER_HUB_USER}/hermes-gateway-sidecar:latest .
	rm -rf /tmp/hermes

run_cl: ## local run cl dependency
	docker compose -f docker-compose-ci.yml down -v --remove-orphans
	docker compose -f docker-compose-ci.yml up -d

run_gateway_with_sidecar: ## Run gateway with Hermes as sidecar
	docker compose -f docker-compose-sidecar.yml down -v --remove-orphans
	docker compose -f docker-compose-sidecar.yml up --build

logs_prysm: ## Show logs of prysm service
	docker logs -f prysm-beacon

run:
	go run cmd/main.go -config config/app_conf.yml

test: ## Runs tests (unit tests and integration tests)
	${info Running tests...}
	go test -v ./... -cover -coverprofile cover.out
	sed -i '/test_utils\//d' cover.out
	sed -i '/fastssz_codegen\//d' cover.out
	sed -i '/\.pb\.go:/d' cover.out
	go tool cover -func cover.out | grep total

coverage: ## Check test coverage is enough
	@echo "Threshold:                ${COVERAGE_THRESHOLD}%"
	@echo "Current test coverage is: ${COVERAGE_TOTAL}%"
	@if [ "${COVERAGE_PASS_THRESHOLD}" -eq "0" ] ; then \
		echo "Test coverage is lower than threshold"; \
		exit 1; \
	fi

build: ## Builds binary
	@echo "-- building binary"
	go build -o ./bin/optimum-gateway ./cmd

GO_LICENSES_RUN := go run github.com/google/go-licenses@v1.6.0
CYCLONEDX_RUN   := go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0

license-check: ## Check shipped binary (./cmd) deps for strong copyleft (strict)
	@echo "Running license check (shipped binary)..."
	@set -o pipefail; \
		$(GO_LICENSES_RUN) csv ./cmd \
		--ignore github.com/getoptimum 2>/dev/null | \
		python3 scripts/check-licenses.py --config licenses.yaml --strict-unknown

license-check-test: ## Report test/infra (./...) deps (report-only, never fails)
	@echo "Running license report (test/infra deps)..."
	@# no pipefail: a go-licenses failure must not gate this report-only target;
	@# the (always-0) classifier exit code is what matters.
	@$(GO_LICENSES_RUN) csv ./... \
		--ignore github.com/getoptimum 2>/dev/null | \
		python3 scripts/check-licenses.py --config licenses.yaml --report-only

notices: ## Regenerate THIRD-PARTY-NOTICES.md from shipped binary (./cmd) deps
	@echo "Generating third-party notices..."
	@set -o pipefail; \
		$(GO_LICENSES_RUN) csv ./cmd \
		--ignore github.com/getoptimum 2>/dev/null | \
		python3 scripts/gen-notices.py --config licenses.yaml --output THIRD-PARTY-NOTICES.md

sbom: sbom-binary sbom-full ## Regenerate both SBOMs

# -noserial -notimestamp plus scripts/normalize-sbom.py keep output stable across
# commits and machines, so the CI sbom-refresh job only commits on real
# dependency changes, not on every push.
sbom-binary: ## Binary-footprint SBOM (shipped runtime deps) -> docs/sbom.json
	@echo "Generating binary-footprint SBOM (app mode)..."
	@$(CYCLONEDX_RUN) app -main ./cmd -licenses -noserial -notimestamp -json -output docs/sbom.json .
	@python3 scripts/normalize-sbom.py docs/sbom.json

sbom-full: ## Full SBOM (all go.mod + tool-block deps) -> docs/sbom-full.json
	@echo "Generating full SBOM (mod mode)..."
	@$(CYCLONEDX_RUN) mod -licenses -noserial -notimestamp -json -output docs/sbom-full.json .
	@python3 scripts/normalize-sbom.py docs/sbom-full.json

start_grafana: ## Start grafana service
	@echo "-- starting grafana"
	docker compose -f grafana/docker-compose-grafana.yml up --build

lint: ## Runs linters
	go tool golangci-lint run ./...

proto: ## Generate Protobuf files
	@echo "⚙️  Formatting Protobuf files..."
	@go tool buf format -w
	@echo "⚙️  Linting Protobuf files..."
	@go tool buf lint
	@echo "⚙️  Generating Protobuf files..."
	@go tool buf generate

# Upstream ships the spectests package pre-generated, so vendoring is a copy
# from the go.mod-pinned module; test files and unused test helpers are dropped.
fastssz-generate: ## Vendor fastssz spectests SSZ types into pkg/protocol/fastssz_codegen
	@go mod download github.com/ferranbt/fastssz
	@src="$$(go list -m -f '{{.Dir}}' github.com/ferranbt/fastssz)/spectests" && \
		cp "$$src"/*.go pkg/protocol/fastssz_codegen/ && \
		chmod 0644 pkg/protocol/fastssz_codegen/*.go
	@rm -f pkg/protocol/fastssz_codegen/*_test.go pkg/protocol/fastssz_codegen/cmp.go
	@echo "fastssz code vendored at pkg/protocol/fastssz_codegen/"

.PHONY: fastssz-generate
.PHONY: help test lint coverage vulcheck build deps proto run run_cl build_hermes_image run_gateway_with_sidecar license-check license-check-test notices sbom sbom-binary sbom-full
.DEFAULT_GOAL := help
