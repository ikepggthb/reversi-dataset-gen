# reversi-dataset-gen build & engine setup.
#
# 主要ターゲット:
#   build           rdg バイナリをビルド
#   test            全テスト
#   fmt vet         整形 / 静的解析
#   setup-edax      Edax を clone + build し eval.dat を配置
#   setup-egaroucid Egaroucid を clone + build
#   setup           上記両方
#   clean-engines   third_party を削除

GO       ?= go
RDG_BIN  := rdg
ROOT     := $(CURDIR)
TP_DIR   := $(ROOT)/third_party

# ---- Edax (source build + release eval.dat) ----
#
# source から binary を build する。eval.dat は source build では生成されないため、
# 公式 release tarball を sha256 検証して data/eval.dat だけ取り出す。

EDAX_VERSION := 4.6
EDAX_REPO    := https://github.com/abulmo/edax-reversi.git
EDAX_REF     := 14f048c05ddfa385b6bf954a9c2905bbe677e9d3
EDAX_ARCH    ?= x86-64-v3
EDAX_CC      ?= clang
EDAX_TARBALL := edax-$(EDAX_VERSION)-linux-x86.tar.gz
EDAX_URL     := https://github.com/abulmo/edax-reversi/releases/download/v$(EDAX_VERSION)/$(EDAX_TARBALL)
EDAX_SHA256  := 7ca52cb0ccf591ad9690e7d21861d4a6f04b900c686346db98ea26dd30cd966a
EDAX_EVAL_SHA256 := f8b2299612d9fa4414157e70e932636e33111c2602d0c2fc382a7d90ef21b792
EDAX_DIR     := $(TP_DIR)/edax-reversi
EDAX_BIN     := $(EDAX_DIR)/bin/lEdax-$(EDAX_ARCH)
EDAX_EVAL    := $(EDAX_DIR)/bin/data/eval.dat

# ---- Egaroucid ----
#
# Linux 用は source から Egaroucid for Console を直接 clang++ でビルドする。

EGAROUCID_REPO := https://github.com/Nyanyan/Egaroucid.git
EGAROUCID_REF  := ca813afe2512e2d939ad22dbd53f43bb2c1e55f8
EGAROUCID_CXX  ?= clang++
EGAROUCID_DIR  := $(TP_DIR)/Egaroucid
EGAROUCID_BIN  := $(EGAROUCID_DIR)/bin/Egaroucid_for_Console.out
EGAROUCID_EVAL := $(EGAROUCID_DIR)/bin/resources/eval.egev2

# ===========================================================================
# 一般
# ===========================================================================

.PHONY: build test fmt vet
build:
	$(GO) build -o $(RDG_BIN) ./cmd/rdg

test:
	$(GO) test ./...

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './third_party/*')

vet:
	$(GO) vet ./...

# ===========================================================================
# Edax setup (公式リリース取得)
# ===========================================================================

.PHONY: setup-edax
setup-edax: | $(TP_DIR)
	@if [ -d "$(EDAX_DIR)/.git" ]; then \
		echo "==> Updating Edax source in $(EDAX_DIR)"; \
	elif [ -e "$(EDAX_DIR)" ]; then \
		echo "ERROR: $(EDAX_DIR) exists but is not a git checkout. Move or delete it, then rerun setup-edax."; \
		exit 1; \
	else \
		echo "==> Cloning Edax"; \
		git clone "$(EDAX_REPO)" "$(EDAX_DIR)"; \
	fi
	@if [ "$$(cd "$(EDAX_DIR)" && git rev-parse HEAD)" = "$(EDAX_REF)" ]; then \
		echo "==> Edax already at $(EDAX_REF)"; \
	else \
		cd "$(EDAX_DIR)" && git fetch --tags origin && git checkout "$(EDAX_REF)"; \
		rm -f "$(EDAX_BIN)"; \
	fi
	mkdir -p "$(EDAX_DIR)/bin/data"
	@if [ -x "$(EDAX_BIN)" ]; then \
		echo "==> Edax binary already exists: $(EDAX_BIN)"; \
	else \
		$(MAKE) -C "$(EDAX_DIR)/src" build ARCH=$(EDAX_ARCH) CC=$(EDAX_CC) OS=linux; \
	fi
	@if [ ! -f "$(EDAX_EVAL)" ] || ! echo "$(EDAX_EVAL_SHA256)  $(EDAX_EVAL)" | sha256sum -c - >/dev/null 2>&1; then \
		tmp="$$(mktemp -d "$(TP_DIR)/edax-eval.XXXXXX")"; \
		echo "==> Downloading Edax eval data"; \
		curl -fSL "$(EDAX_URL)" -o "$$tmp/$(EDAX_TARBALL)"; \
		echo "$(EDAX_SHA256)  $$tmp/$(EDAX_TARBALL)" | sha256sum -c -; \
		tar xzf "$$tmp/$(EDAX_TARBALL)" -C "$$tmp" data/eval.dat; \
		cp "$$tmp/data/eval.dat" "$(EDAX_EVAL)"; \
		rm -rf "$$tmp"; \
	fi
	echo "$(EDAX_EVAL_SHA256)  $(EDAX_EVAL)" | sha256sum -c -
	test -x "$(EDAX_BIN)" || (echo "ERROR: $(EDAX_BIN) is not executable"; exit 1)
	@echo "==> Edax ready: $(EDAX_BIN)"

$(TP_DIR):
	mkdir -p $(TP_DIR)

# ===========================================================================
# Egaroucid setup (clone + build)
# ===========================================================================

.PHONY: setup-egaroucid
setup-egaroucid: | $(TP_DIR)
	@if [ -d "$(EGAROUCID_DIR)/.git" ]; then \
		echo "==> Updating Egaroucid source in $(EGAROUCID_DIR)"; \
	elif [ -e "$(EGAROUCID_DIR)" ]; then \
		echo "ERROR: $(EGAROUCID_DIR) exists but is not a git checkout. Move or delete it, then rerun setup-egaroucid."; \
		exit 1; \
	else \
		echo "==> Cloning Egaroucid"; \
		git clone "$(EGAROUCID_REPO)" "$(EGAROUCID_DIR)"; \
	fi
	@if [ "$$(cd "$(EGAROUCID_DIR)" && git rev-parse HEAD)" = "$(EGAROUCID_REF)" ]; then \
		echo "==> Egaroucid already at $(EGAROUCID_REF)"; \
	else \
		cd "$(EGAROUCID_DIR)" && git fetch --tags origin && git checkout "$(EGAROUCID_REF)"; \
		rm -f "$(EGAROUCID_BIN)"; \
	fi
	mkdir -p "$(EGAROUCID_DIR)/bin"
	@if [ -x "$(EGAROUCID_BIN)" ]; then \
		echo "==> Egaroucid binary already exists: $(EGAROUCID_BIN)"; \
	else \
		cd "$(EGAROUCID_DIR)" && $(EGAROUCID_CXX) -O2 ./src/Egaroucid_for_Console.cpp -o ./bin/Egaroucid_for_Console.out -mtune=native -march=native -pthread -std=c++20; \
	fi
	test -x "$(EGAROUCID_BIN)" || (echo "ERROR: $(EGAROUCID_BIN) is not executable"; exit 1)
	test -f "$(EGAROUCID_EVAL)" || (echo "ERROR: $(EGAROUCID_EVAL) not found"; exit 1)
	@echo "==> Egaroucid ready: $(EGAROUCID_BIN)"

# ===========================================================================
# 統合 / smoke
# ===========================================================================

.PHONY: setup
setup: setup-edax setup-egaroucid

# ===========================================================================
# Clean
# ===========================================================================

.PHONY: clean-engines clean
clean-engines:
	rm -rf $(TP_DIR)

clean:
	rm -f $(RDG_BIN)
