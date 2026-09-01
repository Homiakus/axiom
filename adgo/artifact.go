package adgo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ArtifactStore interface {
	Put(name, mediaType string, r io.Reader) (ArtifactRef, error)
	Open(ArtifactRef) (io.ReadCloser, error)
	Exists(ArtifactRef) bool
}

type ContentAddressedStore struct{ root string }

func NewContentAddressedStore(root string) (*ContentAddressedStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("adgo: artifact root is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "sha256"), privateStateDirMode); err != nil {
		return nil, err
	}
	return &ContentAddressedStore{root: root}, nil
}

func (s *ContentAddressedStore) Put(name, mediaType string, r io.Reader) (ArtifactRef, error) {
	tmp, err := os.CreateTemp(s.root, "artifact-*")
	if err != nil {
		return ArtifactRef{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		_ = tmp.Close()
		return ArtifactRef{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return ArtifactRef{}, err
	}
	if err := tmp.Close(); err != nil {
		return ArtifactRef{}, err
	}
	digest := hex.EncodeToString(h.Sum(nil))
	dir := filepath.Join(s.root, "sha256", digest[:2])
	if err := os.MkdirAll(dir, privateStateDirMode); err != nil {
		return ArtifactRef{}, err
	}
	path := filepath.Join(dir, digest)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.Rename(tmpName, path); err != nil {
			return ArtifactRef{}, err
		}
		if err := syncDir(dir); err != nil {
			return ArtifactRef{}, err
		}
	} else if err != nil {
		return ArtifactRef{}, err
	}
	uri := "artifact://sha256/" + digest
	if name != "" {
		uri += "?name=" + safeName(name)
	}
	return ArtifactRef{URI: uri, Digest: "sha256:" + digest, Size: n, MediaType: mediaType}, nil
}

func canonicalArtifactDigest(ref ArtifactRef) (string, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(ref.Digest, prefix) {
		return "", fmt.Errorf("adgo: invalid artifact digest %q", ref.Digest)
	}
	digest := strings.TrimPrefix(ref.Digest, prefix)
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return "", fmt.Errorf("adgo: invalid artifact digest %q", ref.Digest)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("adgo: invalid artifact digest %q", ref.Digest)
	}
	return digest, nil
}

func (s *ContentAddressedStore) relativePath(ref ArtifactRef) (string, error) {
	digest, err := canonicalArtifactDigest(ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(digest[:2], digest), nil
}

func (s *ContentAddressedStore) Open(ref ArtifactRef) (io.ReadCloser, error) {
	rel, err := s.relativePath(ref)
	if err != nil {
		return nil, err
	}
	return os.OpenInRoot(filepath.Join(s.root, "sha256"), rel)
}

func (s *ContentAddressedStore) Exists(ref ArtifactRef) bool {
	rel, err := s.relativePath(ref)
	if err != nil {
		return false
	}
	root, err := os.OpenRoot(filepath.Join(s.root, "sha256"))
	if err != nil {
		return false
	}
	defer root.Close()
	_, err = root.Stat(rel)
	return err == nil
}
