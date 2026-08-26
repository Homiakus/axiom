package adgo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type fileLockRecord struct {
	Owner      string    `json:"owner"`
	AcquiredAt time.Time `json:"acquiredAt"`
}

func newFileLockOwner() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func writeFileLockRecord(file *os.File, record fileLockRecord) error {
	return json.NewEncoder(file).Encode(record)
}

func readFileLockRecord(path string) (fileLockRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileLockRecord{}, err
	}
	var record fileLockRecord
	if err := json.Unmarshal(data, &record); err == nil && strings.TrimSpace(record.Owner) != "" {
		return record, nil
	}

	// Backward compatibility with pre-owner-token lock files that contained
	// only the acquisition UnixNano timestamp. They may still exist after a
	// process crash during an in-place upgrade and must remain reclaimable.
	legacy := strings.TrimSpace(string(data))
	if _, err := strconv.ParseInt(legacy, 10, 64); err == nil {
		return fileLockRecord{Owner: "legacy:" + legacy}, nil
	}
	return fileLockRecord{}, errors.New("adgo: malformed file-store lock record")
}

// withOwnedFileLock executes fn while filename is held by a unique ownership
// token. The token is checked on release and a heartbeat keeps long-running
// live owners from being reclaimed as stale. All file-backed coordination
// primitives should use this helper instead of implementing anonymous lock-file
// removal independently.
func withOwnedFileLock(ctx context.Context, locksDir, filename string, staleAfter time.Duration, fn func() error) error {
	if err := os.MkdirAll(locksDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(locksDir, filename)
	owner, err := newFileLockOwner()
	if err != nil {
		return err
	}
	for {
		file, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if openErr == nil {
			record := fileLockRecord{Owner: owner, AcquiredAt: time.Now().UTC()}
			if err := writeFileLockRecord(file, record); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return err
			}
			if err := file.Sync(); err != nil {
				_ = file.Close()
				_ = releaseFileLock(path, owner)
				return err
			}
			if err := syncDir(locksDir); err != nil {
				_ = file.Close()
				_ = releaseFileLock(path, owner)
				return err
			}

			heartbeat := startFileLockHeartbeat(file, path, owner, staleAfter)
			cleaned := false
			cleanup := func() error {
				heartbeatErr := heartbeat.Stop()
				closeErr := file.Close()
				releaseErr := releaseFileLock(path, owner)
				syncErr := syncDir(locksDir)
				if heartbeatErr != nil {
					return heartbeatErr
				}
				if closeErr != nil {
					return closeErr
				}
				if releaseErr != nil {
					return releaseErr
				}
				return syncErr
			}
			defer func() {
				if !cleaned {
					_ = cleanup()
				}
			}()

			fnErr := fn()
			cleanupErr := cleanup()
			cleaned = true
			if fnErr != nil {
				return fnErr
			}
			return cleanupErr
		}
		if !errors.Is(openErr, fs.ErrExist) {
			return openErr
		}
		removed, staleErr := removeStaleFileLock(path, staleAfter)
		if staleErr != nil {
			return staleErr
		}
		if removed {
			if err := syncDir(locksDir); err != nil {
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// releaseFileLock removes a lock only while the path still belongs to owner.
// This prevents a previous holder from deleting a replacement lock after a
// stale-lock takeover (the classic lock-file ABA failure mode).
func releaseFileLock(path, owner string) error {
	record, err := readFileLockRecord(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Owner != owner {
		return nil
	}
	return os.Remove(path)
}

// removeStaleFileLock reclaims a stale lock only if the same filesystem object
// and owner record are still observed immediately before removal. The final
// unlink cannot be made compare-and-delete atomically with portable os APIs,
// but the double identity check closes the practical replacement window and,
// together with ownership-aware release, eliminates the known ABA sequence.
func removeStaleFileLock(path string, staleAfter time.Duration) (bool, error) {
	infoBefore, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if time.Since(infoBefore.ModTime()) <= staleAfter {
		return false, nil
	}
	recordBefore, err := readFileLockRecord(path)
	if err != nil {
		return false, err
	}

	infoAfter, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	recordAfter, err := readFileLockRecord(path)
	if err != nil {
		return false, err
	}
	if !os.SameFile(infoBefore, infoAfter) || recordBefore.Owner != recordAfter.Owner {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
