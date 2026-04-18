# Makefile for oapi-gen code generation
# Generates types and chi-server HTTP handlers from 3GPP OpenAPI specs.
#
# Prerequisites:
#   go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# ─── Paths ────────────────────────────────────────────────────────────────────
OAPI_CODEGEN  := $(shell which oapi-codegen 2>/dev/null || echo "$(HOME)/go/bin/oapi-codegen")
SPEC_DIR      := $(shell pwd)/specs
GEN_DIR       := $(shell pwd)/gen

# Individual outputs
NSSAA_SPEC    := $(SPEC_DIR)/TS29526_Nnssaaf_NSSAA.yaml
AIW_SPEC      := $(SPEC_DIR)/TS29526_Nnssaaf_AIW.yaml
NSSAA_CFG     := $(shell pwd)/nssaa_config.yaml
AIW_CFG       := $(shell pwd)/aiw_config.yaml
NSSAA_OUT     := $(GEN_DIR)/nssaa/nssaa.gen.go
AIW_OUT       := $(GEN_DIR)/aiw/aiw.gen.go

.PHONY: all clean generate generate-nssaa generate-aiw tidy-deps verify-deps help

# ─── Default target ───────────────────────────────────────────────────────────
all: verify-deps generate tidy-deps
	@echo ""
	@echo "done — generated:"
	@echo "  $(NSSAA_OUT)"
	@echo "  $(AIW_OUT)"

# ─── Generate both APIs ───────────────────────────────────────────────────────
generate: generate-nssaa generate-aiw

generate-nssaa: $(NSSAA_OUT)
	@echo "[nssaa] generated -> $(NSSAA_OUT)"

generate-aiw: $(AIW_OUT)
	@echo "[aiw]   generated -> $(AIW_OUT)"

# ─── Individual generation rules ───────────────────────────────────────────────
$(NSSAA_OUT): $(NSSAA_SPEC) $(NSSAA_CFG)
	@echo "==> generating NSSAA types + chi-server handlers from $(NSSAA_SPEC)"
	@mkdir -p $(GEN_DIR)/nssaa
	$(OAPI_CODEGEN) --config $(NSSAA_CFG) $(NSSAA_SPEC)

$(AIW_OUT): $(AIW_SPEC) $(AIW_CFG)
	@echo "==> generating AIW types + chi-server handlers from $(AIW_SPEC)"
	@mkdir -p $(GEN_DIR)/aiw
	$(OAPI_CODEGEN) --config $(AIW_CFG) $(AIW_SPEC)

# ─── Dependency management ────────────────────────────────────────────────────
tidy-deps:
	@echo "==> running go mod tidy in gen modules"
	cd $(GEN_DIR)/specs && go mod tidy
	cd $(GEN_DIR)/nssaa && go mod tidy
	cd $(GEN_DIR)/aiw   && go mod tidy

# ─── Verification ─────────────────────────────────────────────────────────────
verify-deps:
	@echo "==> checking oapi-codegen availability"
	@if [ ! -x "$(OAPI_CODEGEN)" ]; then \
		echo "ERROR: oapi-codegen not found. Install with:"; \
		echo "  go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest"; \
		exit 1; \
	fi
	@echo "    oapi-codegen: $(OAPI_CODEGEN)"

# ─── Clean ────────────────────────────────────────────────────────────────────
clean:
	@echo "==> removing generated files"
	rm -f $(NSSAA_OUT) $(AIW_OUT)

# ─── Help ──────────────────────────────────────────────────────────────────────
help:
	@echo "Targets:"
	@echo "  all            – verify deps, generate both APIs, tidy go modules  (default)"
	@echo "  generate       – generate both NSSAA and AIW handlers"
	@echo "  generate-nssaa – generate NSSAA types + chi-server handlers only"
	@echo "  generate-aiw   – generate AIW types + chi-server handlers only"
	@echo "  tidy-deps      – run go mod tidy in all gen submodules"
	@echo "  verify-deps    – check that oapi-codegen is installed"
	@echo "  clean          – remove generated .gen.go files"
	@echo "  help           – show this message"
