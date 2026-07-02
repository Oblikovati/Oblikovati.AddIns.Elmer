# Build the oblikovati-elmer add-in as a c-shared library (.so/.dll/.dylib) and
# install it into the host's add-ins directory, which the host scans at startup.
#
# Its own Go toolchain: the add-in talks to the host over the C ABI, not Go, so its
# version is independent of the host (pinned to match the head's 1.24, see go.mod).
export GOTOOLCHAIN := go1.24.0
export CGO_ENABLED := 1

OS := $(shell go env GOOS)
EXT := so
ifeq ($(OS),windows)
	EXT := dll
endif
ifeq ($(OS),darwin)
	EXT := dylib
endif

LIB := oblikovati-elmer.$(EXT)
OUT ?= build
# Where the host looks for add-ins (the head in the sibling app repo; OBK_ADDINS_DIR
# overrides at run). The app is a SIBLING of this repo — one `..` — matching the
# `use ../Oblikovati` in go.work.
ADDINS_DIR ?= ../Oblikovati/head/addins

# The C ABI header is owned by the public oblikovati.org/api module (its source of truth);
# we vendor it into ./include (git-ignored) so cgo can -I it. Resolving the module dir
# with `go list -m` works both under go.work (local dev) and the CI -replace.
HDR := include/oblikovati_addin.h

.PHONY: build lib install test clean sync-header build-solvers

sync-header: $(HDR) ## Vendor the C ABI header from the oblikovati.org/api module
$(HDR):
	@mkdir -p include
	@api_dir=$$(go list -m -f '{{.Dir}}' oblikovati.org/api) && \
		cp "$$api_dir/include/oblikovati_addin.h" "$(HDR)" && \
		echo "synced $(HDR) <- $$api_dir/include"

build: sync-header ## Build the c-shared add-in into $(OUT)/
	mkdir -p $(OUT)
	go build -buildmode=c-shared -o $(OUT)/$(LIB) .

lib: build ## Alias for `build` (matches the M1 scaffold's smoke-test target name)

# Build the vendored solver toolchain the engine runs at arm's length, fully from the
# in-repo sources (no network, no system libraries):
#   gmsh      = gmsh 4.13.1 CLI (bundled meshing engines)      -> vendor-src/gmsh/build/gmsh
#   elmerfem  = headless ElmerSolver                           -> vendor-src/elmerfem/build/ElmerSolver
# The requireSolver-gated tests look there (or at OBK_GMSH_BIN / OBK_ELMERSOLVER_BIN). Needs a
# C/C++ compiler + gfortran + cmake (build-time only). Populated by later tasks (vendor gmsh,
# vendor elmerfem); the build.sh scripts don't exist yet in this scaffold.
build-solvers: ## Build the vendored gmsh + ElmerSolver binaries from source
	vendor-src/gmsh/build.sh
	vendor-src/elmerfem/build.sh

install: build ## Build then copy the library + manifest into $(ADDINS_DIR)
	mkdir -p $(ADDINS_DIR)
	cp $(OUT)/$(LIB) $(ADDINS_DIR)/
	cp manifest.json $(ADDINS_DIR)/
	@echo "installed $(LIB) -> $(ADDINS_DIR)"

test: sync-header ## Run the add-in tests (pure-Go elmer engine + full-stack E2E)
	go test ./...

clean: ## Remove build outputs and the vendored header
	rm -rf $(OUT) include
