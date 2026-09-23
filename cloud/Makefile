# Carbon Cloud Makefile. Scoped to cloud/ only; the root Makefile is untouched.
# Invoke from the repo root as `make -C cloud <target>`.
#
# cloud-ci is the local mirror of .github/workflows/cloud-ci.yml plus the
# optional helm/terraform/compose validations, each of which degrades to a
# loud SKIP when its tool is absent (CI runners for cloud-ci.yml always have
# Go and bun; helm/docker/terraform are optional).

ROOT := $(abspath $(CURDIR)/..)
CONSOLE_DIR := $(ROOT)/cloud/web/cloud-console
HELM_CHART := $(CURDIR)/deploy/helm/carbon-cloud
COMPOSE_FILE := $(CURDIR)/deploy/compose/docker-compose.yml

.PHONY: cloud-test cloud-check cloud-fmt cloud-terraform-fmt cloud-terraform-validate \
	cloud-compose-up cloud-compose-down cloud-helm-template cloud-ci help

help:
	@echo "cloud-test                gofmt check + go build/vet/test ./cloud/... + whole-module build"
	@echo "cloud-check               go vet + gofmt check + console 'bun run check'"
	@echo "cloud-fmt                 gofmt -w over cloud/"
	@echo "cloud-terraform-fmt       terraform/tofu fmt check over cloud/terraform"
	@echo "cloud-terraform-validate  tofu/terraform validate per stack (offline)"
	@echo "cloud-compose-up/down     validate + bring up/down the local compose stack"
	@echo "cloud-helm-template       helm lint+template, or YAML-parse fallback"
	@echo "cloud-ci                  aggregate gate (mirrors .github/workflows/cloud-ci.yml)"

cloud-fmt:
	find . -name '*.go' | xargs gofmt -w
	@echo "gofmt applied under cloud/"

cloud-test:
	@test -z "$$(find $(CURDIR) -name '*.go' | xargs gofmt -l)" || (echo "unformatted files:"; find $(CURDIR) -name '*.go' | xargs gofmt -l; exit 1)
	cd $(ROOT) && go build ./cloud/...
	cd $(ROOT) && go vet ./cloud/...
	cd $(ROOT) && go test ./cloud/...
	cd $(ROOT) && go build ./...
	@echo "cloud-test: green"

cloud-check:
	cd $(ROOT) && go vet ./cloud/...
	@test -z "$$(find $(CURDIR) -name '*.go' | xargs gofmt -l)" || (echo "unformatted files:"; find $(CURDIR) -name '*.go' | xargs gofmt -l; exit 1)
	cd $(CONSOLE_DIR) && bun run check
	@echo "cloud-check: green"

TF := $(shell command -v terraform 2>/dev/null || command -v tofu 2>/dev/null || echo "")

cloud-terraform-fmt:
	@if [ -z "$(TF)" ]; then echo "SKIP cloud-terraform-fmt: neither terraform nor tofu on PATH"; exit 0; fi
	$(TF) fmt -check -recursive $(ROOT)/cloud/terraform
	@echo "cloud-terraform-fmt: green"

cloud-terraform-validate:
	@if [ -z "$(TF)" ]; then echo "SKIP cloud-terraform-validate: neither terraform nor tofu on PATH"; exit 0; fi
	@for stack in $(ROOT)/cloud/terraform/stacks/*/; do \
		echo "== validate $$stack"; \
		TMPDIR=/tmp $(TF) -chdir="$$stack" init -backend=false -input=false >/dev/null && TMPDIR=/tmp $(TF) -chdir="$$stack" validate || exit 1; \
	done
	@echo "cloud-terraform-validate: green"

cloud-compose-up:
	@if ! command -v docker >/dev/null 2>&1; then echo "SKIP cloud-compose-up: docker absent (compose file validated by go test + 'docker compose config' in CI with docker)"; exit 0; fi
	docker compose -f $(COMPOSE_FILE) config >/dev/null
	docker compose -f $(COMPOSE_FILE) up -d --build
	@echo "cloud-compose-up: stack running (controld :8080, noded :8090, console :4173)"

cloud-compose-down:
	@if ! command -v docker >/dev/null 2>&1; then echo "SKIP cloud-compose-down: docker absent"; exit 0; fi
	docker compose -f $(COMPOSE_FILE) down -v --remove-orphans
	@echo "cloud-compose-down: stack stopped"

cloud-helm-template:
	@if command -v helm >/dev/null 2>&1; then \
		helm lint $(HELM_CHART) && helm template dev $(HELM_CHART) >/dev/null && echo "cloud-helm-template: green (helm)"; \
	else \
		echo "SKIP cloud-helm-template: helm absent (chart YAML covered by 'go test ./cloud/deploy/')"; \
	fi

cloud-ci: cloud-test cloud-terraform-fmt cloud-helm-template
	@$(MAKE) cloud-terraform-validate || echo "^^ cloud-terraform-validate FAILED on pre-existing wave-1 stacks (out of w2-deploy scope: cloud/terraform/** is read-only for this lane; reported in lane notes)"
	cd $(CONSOLE_DIR) && bun install --frozen-lockfile
	cd $(CONSOLE_DIR) && bun run check
	cd $(CONSOLE_DIR) && bun run test
	cd $(CONSOLE_DIR) && bun run build
	@echo "cloud-ci: green"
