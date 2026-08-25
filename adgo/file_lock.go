package adgo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
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
