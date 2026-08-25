package adgo

import (
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"
)

var errFileLockLost = errors.New("adgo: file-store lock ownership lost")

type fileLockHeartbeat struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func fileLockHeartbeatInterval(staleAfter time.Duration) time.Duration {
	if staleAfter <= 0 {
		return 10 * time.Second
	}
	interval := staleAfter / 3
	if interval <= 0 {
		return time.Nanosecond
	}
	return interval
}

// refreshFileLock updates the mtime of the exact inode acquired by owner. The
// owner/path identity check is performed first so a delayed heartbeat can never
// refresh a replacement lock created after stale takeover.
func refreshFileLock(file *os.File, path, owner string) error {
	ownedInfo, err := file.Stat()
	if err != nil {
		return err
	}
	pathInfo, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return errFileLockLost
	}
	if err != nil {
		return err
	}
	if !os.SameFile(ownedInfo, pathInfo) {
		return errFileLockLost
	}
	record, err := readFileLockRecord(path)
	if err != nil {
		return err
	}
	if record.Owner != owner {
		return errFileLockLost
	}
	if ownedInfo.Size() == 0 {
		return errors.New("adgo: empty file-store lock record")
	}

	// writeFileLockRecord uses json.Encoder, so the final byte is a newline.
	// Rewriting that same byte preserves the record while refreshing the inode
	// mtime used by stale-lock detection.
	if _, err := file.WriteAt([]byte{'\n'}, ownedInfo.Size()-1); err != nil {
		return err
	}
	return file.Sync()
}

func startFileLockHeartbeat(file *os.File, path, owner string, staleAfter time.Duration) *fileLockHeartbeat {
	h := &fileLockHeartbeat{stop: make(chan struct{}), done: make(chan struct{})}
	interval := fileLockHeartbeatInterval(staleAfter)
	go func() {
		defer close(h.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-ticker.C:
				if err := refreshFileLock(file, path, owner); err != nil {
					h.mu.Lock()
					h.err = err
					h.mu.Unlock()
					return
				}
			}
		}
	}()
	return h
}

func (h *fileLockHeartbeat) Stop() error {
	if h == nil {
		return nil
	}
	h.once.Do(func() { close(h.stop) })
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}
