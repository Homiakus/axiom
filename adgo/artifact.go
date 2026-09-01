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
func (s *ContentAddressedStore) path(ref ArtifactRef) (string, error) {
	digest := strings.TrimPrefix(ref.Digest, "sha256:")
	if len(digest) != 64 {
		return "", fmt.Errorf("adgo: invalid artifact digest %q", ref.Digest)
	}
	return filepath.Join(s.root, "sha256", digest[:2], digest), nil
}
func (s *ContentAddressedStore) Open(ref ArtifactRef) (io.ReadCloser, error) {
	path, err := s.path(ref)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}
func (s *ContentAddressedStore) Exists(ref ArtifactRef) bool {
	path, err := s.path(ref)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
