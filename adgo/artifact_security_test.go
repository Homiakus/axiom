package adgo

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentAddressedStoreRejectsNonCanonicalDigest(t *testing.T) {
	store, err := NewContentAddressedStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"",
		"sha256:",
		"md5:" + strings.Repeat("a", 64),
		"sha256:" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("a", 65),
		"sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("a", 63) + "g",
		"sha256:" + strings.Repeat("a", 31) + "/" + strings.Repeat("b", 32),
		"sha256:" + strings.Repeat("a", 31) + `\` + strings.Repeat("b", 32),
	}

	for _, digest := range cases {
		digest := digest
		t.Run(digest, func(t *testing.T) {
			ref := ArtifactRef{Digest: digest}
			if store.Exists(ref) {
				t.Fatalf("Exists accepted non-canonical digest %q", digest)
			}
			file, err := store.Open(ref)
			if err == nil {
				_ = file.Close()
				t.Fatalf("Open accepted non-canonical digest %q", digest)
			}
		})
	}
}

func TestContentAddressedStoreCanonicalDigestRoundTrip(t *testing.T) {
	store, err := NewContentAddressedStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put("payload.txt", "text/plain", bytes.NewBufferString("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if !store.Exists(ref) {
		t.Fatal("stored artifact does not exist")
	}
	file, err := store.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("artifact payload = %q, want payload", data)
	}
}

func TestContentAddressedStoreRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	store, err := NewContentAddressedStore(base)
	if err != nil {
		t.Fatal(err)
	}

	digest := strings.Repeat("a", 64)
	dir := filepath.Join(base, "sha256", digest[:2])
	if err := os.MkdirAll(dir, privateStateDirMode); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), privateLockFileMode); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, digest)
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is not available on this platform: %v", err)
	}

	ref := ArtifactRef{Digest: "sha256:" + digest}
	if store.Exists(ref) {
		t.Fatal("Exists followed a symlink outside the artifact root")
	}
	file, err := store.Open(ref)
	if err == nil {
		_ = file.Close()
		t.Fatal("Open followed a symlink outside the artifact root")
	}
}
