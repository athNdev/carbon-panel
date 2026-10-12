package minecraft

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func resetManifestCache(t *testing.T, m *VersionManifestV2) {
	t.Helper()
	cache.mu.Lock()
	prev := cache.manifest
	prevT := cache.lastFetchTime
	cache.manifest = m
	cache.lastFetchTime = time.Now()
	cache.mu.Unlock()
	t.Cleanup(func() {
		cache.mu.Lock()
		cache.manifest = prev
		cache.lastFetchTime = prevT
		cache.mu.Unlock()
	})
}

func releaseTime(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return ts
}

func TestFindMostRecentMinecraftVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		expected string
	}{
		{
			name:     "standard 1.x versions",
			versions: []string{"1.16.5", "1.17.1", "1.18.2", "1.19.4", "1.20.1", "1.21.1"},
			expected: "1.21.1",
		},
		{
			name:     "unsorted 1.x versions",
			versions: []string{"1.21.4", "1.16.5", "1.20.1", "1.7.10"},
			expected: "1.21.4",
		},
		{
			name:     "26.x modern versions higher than 1.x",
			versions: []string{"1.16.5", "1.20.1", "1.21.1", "26.1", "26.1.1", "26.2", "26.3"},
			expected: "26.3",
		},
		{
			name:     "single component minor versions like 1.21",
			versions: []string{"1.20.4", "1.21"},
			expected: "1.21",
		},
		{
			name:     "empty slice",
			versions: []string{},
			expected: "",
		},
		{
			name:     "fallback when strings contain letters or non-semver",
			versions: []string{"fabric-snapshot-1", "custom-alpha"},
			expected: "custom-alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindMostRecentMinecraftVersion(tt.versions)
			if result != tt.expected {
				t.Errorf("FindMostRecentMinecraftVersion(%v) = %q, want %q", tt.versions, result, tt.expected)
			}
		})
	}
}

func TestParseMinecraftReleaseVersion(t *testing.T) {
	tests := []struct {
		in              string
		major, minor, pt int
		ok              bool
	}{
		{"1.21.4", 1, 21, 4, true},
		{"1.21", 1, 21, 0, true},
		{"26.3", 26, 3, 0, true},
		{"1", 0, 0, 0, false},
		{"1.2.3.4", 0, 0, 0, false},
		{"a.2", 0, 0, 0, false},
		{"1.b", 0, 0, 0, false},
		{"0.5", 0, 0, 0, false},
		{"1.-1", 0, 0, 0, false},
		{"1.2.-1", 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			maj, min, pt, ok := parseMinecraftReleaseVersion(tt.in)
			if maj != tt.major || min != tt.minor || pt != tt.pt || ok != tt.ok {
				t.Errorf("parseMinecraftReleaseVersion(%q) = (%d,%d,%d,%v), want (%d,%d,%d,%v)",
					tt.in, maj, min, pt, ok, tt.major, tt.minor, tt.pt, tt.ok)
			}
		})
	}
}

func testManifest() *VersionManifestV2 {
	return &VersionManifestV2{
		Latest: LatestVersions{Release: "1.21.4", Snapshot: "25w41a"},
		Versions: []Version{
			{ID: "1.21.4", Type: "release", URL: "https://example.invalid/1.21.4.json", ReleaseTime: releaseTime("2024-12-03T12:00:00Z")},
			{ID: "25w41a", Type: "snapshot", URL: "https://example.invalid/25w41a.json", ReleaseTime: releaseTime("2025-10-08T12:00:00Z")},
			{ID: "1.20.1", Type: "release", URL: "https://example.invalid/1.20.1.json", ReleaseTime: releaseTime("2023-06-12T12:00:00Z")},
			{ID: "old_alpha", Type: "old_alpha", URL: "https://example.invalid/old.json", ReleaseTime: releaseTime("2010-06-30T12:00:00Z")},
		},
	}
}

func TestManifestQueries(t *testing.T) {
	resetManifestCache(t, testManifest())

	if got := GetLatestVersion(); got != "1.21.4" {
		t.Errorf("GetLatestVersion() = %q, want 1.21.4", got)
	}
	if got := GetLatestSnapshot(); got != "25w41a" {
		t.Errorf("GetLatestSnapshot() = %q, want 25w41a", got)
	}

	releases := GetVersions()
	if len(releases) != 2 || releases[0] != "1.21.4" || releases[1] != "1.20.1" {
		t.Errorf("GetVersions() = %v, want [1.21.4 1.20.1]", releases)
	}

	all := GetAllVersions()
	if len(all) != 4 {
		t.Errorf("GetAllVersions() = %v, want 4 entries", all)
	}

	if !IsValidVersion("1.21.4") {
		t.Error("IsValidVersion(1.21.4) = false, want true")
	}
	if IsValidVersion("9.9.9") {
		t.Error("IsValidVersion(9.9.9) = true, want false")
	}

	if !IsSnapshot("25w41a") {
		t.Error("IsSnapshot(25w41a) = false, want true")
	}
	if IsSnapshot("1.21.4") {
		t.Error("IsSnapshot(1.21.4) = true, want false")
	}
	if IsSnapshot("nope") {
		t.Error("IsSnapshot(nope) = true, want false")
	}

	want := releaseTime("2024-12-03T12:00:00Z")
	got, err := GetVersionDate("1.21.4")
	if err != nil || !got.Equal(want) {
		t.Errorf("GetVersionDate(1.21.4) = (%v, %v), want %v", got, err, want)
	}
	if _, err := GetVersionDate("9.9.9"); err == nil {
		t.Error("GetVersionDate(9.9.9) expected error, got nil")
	}

	info, err := GetVersionInfo("1.20.1")
	if err != nil || info.ID != "1.20.1" {
		t.Errorf("GetVersionInfo(1.20.1) = (%v, %v), want ID 1.20.1", info, err)
	}
	if _, err := GetVersionInfo("9.9.9"); err == nil {
		t.Error("GetVersionInfo(9.9.9) expected error, got nil")
	}
}

func TestManifestErrorPaths(t *testing.T) {
	// Point the manifest URL at a server that always fails so this test
	// never touches the real Mojang network.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	origURL := versionManifestV2URL
	versionManifestV2URL = failing.URL
	t.Cleanup(func() { versionManifestV2URL = origURL })

	// Empty cache that has expired + unreachable URL -> fetch fails.
	cache.mu.Lock()
	cache.manifest = nil
	cache.lastFetchTime = time.Time{}
	cache.mu.Unlock()
	t.Cleanup(func() {
		cache.mu.Lock()
		cache.manifest = nil
		cache.lastFetchTime = time.Time{}
		cache.mu.Unlock()
	})

	if got := GetLatestVersion(); got != "0" {
		t.Errorf("GetLatestVersion() = %q, want 0", got)
	}
	if got := GetLatestSnapshot(); got != "" {
		t.Errorf("GetLatestSnapshot() = %q, want empty", got)
	}
	if got := GetVersions(); len(got) != 0 {
		t.Errorf("GetVersions() = %v, want empty", got)
	}
	if got := GetAllVersions(); len(got) != 0 {
		t.Errorf("GetAllVersions() = %v, want empty", got)
	}
	if IsValidVersion("1.21.4") {
		t.Error("IsValidVersion() = true, want false on fetch error")
	}
	if IsSnapshot("25w41a") {
		t.Error("IsSnapshot() = true, want false on fetch error")
	}
	if _, err := GetVersionDate("1.21.4"); err == nil {
		t.Error("GetVersionDate() expected error on fetch failure")
	}
	if _, err := GetVersionInfo("1.21.4"); err == nil {
		t.Error("GetVersionInfo() expected error on fetch failure")
	}
	if _, err := GetJavaVersion("1.21.4"); err == nil {
		t.Error("GetJavaVersion() expected error on fetch failure")
	}
}

func TestGetJavaVersion(t *testing.T) {
	metadataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"javaVersion":{"component":"java-runtime-delta","majorVersion":21}}`)
	}))
	defer metadataSrv.Close()

	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	manifest := &VersionManifestV2{
		Latest: LatestVersions{Release: "1.21.4"},
		Versions: []Version{
			{ID: "1.21.4", Type: "release", URL: metadataSrv.URL},
			{ID: "broken", Type: "release", URL: badSrv.URL},
			{ID: "missing-meta", Type: "release", URL: "://bad-url"},
		},
	}
	resetManifestCache(t, manifest)
	cache.mu.Lock()
	cache.javaVersions = make(map[string]string)
	cache.mu.Unlock()

	got, err := GetJavaVersion("1.21.4")
	if err != nil || got != "21" {
		t.Fatalf("GetJavaVersion(1.21.4) = (%q, %v), want (21, nil)", got, err)
	}
	// Second call served from javaVersions cache.
	got, err = GetJavaVersion("1.21.4")
	if err != nil || got != "21" {
		t.Fatalf("cached GetJavaVersion(1.21.4) = (%q, %v), want (21, nil)", got, err)
	}

	if _, err := GetJavaVersion("broken"); err == nil {
		t.Error("GetJavaVersion(broken) expected status error")
	}
	if _, err := GetJavaVersion("missing-meta"); err == nil {
		t.Error("GetJavaVersion(missing-meta) expected network error")
	}
	if _, err := GetJavaVersion("nope"); err == nil {
		t.Error("GetJavaVersion(nope) expected not-found error")
	}
}

func TestGetProtocolVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"minecraftVersion":"1.21.4","version":769},{"minecraftVersion":"1.20.1","version":763}]`)
	}))
	defer srv.Close()

	orig := protocolVersionsURL
	protocolVersionsURL = srv.URL
	defer func() { protocolVersionsURL = orig }()

	protoCache.mu.Lock()
	protoCache.versions = make(map[string]int)
	protoCache.lastFetchTime = time.Time{}
	protoCache.mu.Unlock()
	t.Cleanup(func() {
		protoCache.mu.Lock()
		protoCache.versions = make(map[string]int)
		protoCache.lastFetchTime = time.Time{}
		protoCache.mu.Unlock()
	})

	if got := GetProtocolVersion("1.21.4"); got != 769 {
		t.Errorf("GetProtocolVersion(1.21.4) = %d, want 769", got)
	}
	// cached hit
	if got := GetProtocolVersion("1.20.1"); got != 763 {
		t.Errorf("GetProtocolVersion(1.20.1) = %d, want 763", got)
	}
	if got := GetProtocolVersion("9.9.9"); got != -1 {
		t.Errorf("GetProtocolVersion(9.9.9) = %d, want -1", got)
	}
}
