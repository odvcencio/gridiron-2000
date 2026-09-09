package main

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestDockerfileRetainsManifestReferencedTextLayoutAsset(t *testing.T) {
	dockerfile, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	source := string(dockerfile)

	const copyContract = "COPY --from=builder --chown=65532:65532 /src/dist /app/dist"
	if !strings.Contains(source, copyContract) {
		t.Fatalf("runtime stage no longer copies the complete generated dist tree: missing %q", copyContract)
	}

	const prunePattern = "'bootstrap-feature-textlayout*'"
	if strings.Contains(source, prunePattern) {
		t.Fatalf("Dockerfile still prunes the manifest-referenced TextLayout feature asset with %q", prunePattern)
	}

	// Execute the Dockerfile's own pruning RUN block against a temporary
	// dist tree. Keeping the script extracted from the Dockerfile makes this
	// a behavioral contract for the image build, rather than a duplicate
	// implementation of its retention rules.
	const runPrefix = "RUN set -eu; " + "\\" + "\n"
	const runtimeAssignment = "    RUNTIME_DIR=dist/assets/runtime; " + "\\" + "\n"
	runtimeOffset := strings.Index(source, runtimeAssignment)
	if runtimeOffset < 0 {
		t.Fatalf("Dockerfile is missing the runtime prune directory assignment")
	}
	runStart := strings.LastIndex(source[:runtimeOffset], runPrefix)
	if runStart < 0 {
		t.Fatalf("Dockerfile is missing the expected dist-prune RUN block")
	}
	const runEndMarker = "\n\n# Runtime data directory."
	afterRunPrefix := source[runStart+len(runPrefix):]
	runEnd := strings.Index(afterRunPrefix, runEndMarker)
	if runEnd < 0 {
		t.Fatalf("Dockerfile dist-prune RUN block has no expected end marker")
	}
	runBlock := source[runStart : runStart+len(runPrefix)+runEnd]
	script := strings.TrimPrefix(runBlock, "RUN ")
	script = strings.ReplaceAll(script, "\\"+"\n", "\n")

	const assetFile = "bootstrap-feature-textlayout.c072a8fe39b7d2a5.js"
	const assetBody = "window.__gosx_textlayout = true;"
	fixtureRoot := t.TempDir()
	distRoot := filepath.Join(fixtureRoot, "dist")
	runtimeDir := filepath.Join(distRoot, "assets", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("create runtime asset directory: %v", err)
	}
	writeRuntime := func(name string, body []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(runtimeDir, name), body, 0o644); err != nil {
			t.Fatalf("write runtime fixture %q: %v", name, err)
		}
	}
	writeRuntime(assetFile, []byte(assetBody))
	for _, name := range []string{
		"bootstrap-feature-scene3d.fixture.js",
		"gosx-runtime.fixture.wasm",
		"wasm_exec.fixture.js",
		"standard-go-wasm_exec.fixture.js",
		"hls.min.fixture.js",
		"stripe-bridge.fixture.js",
		"relay.fixture.js",
		"keep-runtime.fixture.js",
		"runtime.map",
	} {
		writeRuntime(name, []byte(name))
	}
	appPath := filepath.Join(distRoot, "server", "app")
	if err := os.MkdirAll(filepath.Dir(appPath), 0o755); err != nil {
		t.Fatalf("create server fixture directory: %v", err)
	}
	if err := os.WriteFile(appPath, []byte("duplicate server"), 0o644); err != nil {
		t.Fatalf("write duplicate server fixture: %v", err)
	}

	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapFeatureTextlayout: buildmanifest.HashedAsset{
				File: assetFile,
				Hash: "c072a8fe39b7d2a5",
				Size: int64(len(assetBody)),
			},
		},
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal build manifest fixture: %v", err)
	}
	manifestPath := filepath.Join(distRoot, "build.json")
	if err := os.WriteFile(manifestPath, manifestJSON, 0o644); err != nil {
		t.Fatalf("write build manifest fixture: %v", err)
	}

	loaded, err := buildmanifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("load build manifest fixture: %v", err)
	}
	if loaded.Islands != nil {
		t.Fatalf("fixture must exercise the Dockerfile's islands:null pruning guard")
	}
	runtimeURL := loaded.RuntimeURLs("/gosx/assets").BootstrapFeatureTextlayout
	parsed, err := url.Parse(runtimeURL)
	if err != nil {
		t.Fatalf("parse manifest-derived runtime URL %q: %v", runtimeURL, err)
	}
	const assetURLPrefix = "/gosx/assets/"
	const expectedURLPath = assetURLPrefix + "runtime/" + assetFile
	if parsed.Path != expectedURLPath {
		t.Fatalf("manifest-derived TextLayout URL = %q, want %q", parsed.Path, expectedURLPath)
	}
	relativeAsset := strings.TrimPrefix(parsed.Path, assetURLPrefix)
	if relativeAsset == parsed.Path || filepath.IsAbs(relativeAsset) || strings.Contains(filepath.ToSlash(relativeAsset), "..") {
		t.Fatalf("manifest-derived asset path is not a safe dist-relative path: %q", relativeAsset)
	}
	resolvedAsset := filepath.Join(distRoot, "assets", filepath.FromSlash(relativeAsset))

	command := exec.Command("sh", "-eu", "-c", script)
	command.Dir = fixtureRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("execute Dockerfile dist-prune RUN block: %v\n%s", err, output)
	}

	assertPresent := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected artifact %q to survive pruning: %v", path, err)
		}
		if info.IsDir() {
			t.Fatalf("expected artifact %q to be a file", path)
		}
	}
	assertMissing := func(path string) {
		t.Helper()
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("expected build-only artifact %q to be pruned", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat pruned artifact %q: %v", path, err)
		}
	}
	assertPresent(resolvedAsset)
	assertPresent(filepath.Join(runtimeDir, "keep-runtime.fixture.js"))
	for _, name := range []string{
		"bootstrap-feature-scene3d.fixture.js",
		"gosx-runtime.fixture.wasm",
		"wasm_exec.fixture.js",
		"standard-go-wasm_exec.fixture.js",
		"hls.min.fixture.js",
		"stripe-bridge.fixture.js",
		"relay.fixture.js",
		"runtime.map",
	} {
		assertMissing(filepath.Join(runtimeDir, name))
	}
	assertMissing(appPath)
}
