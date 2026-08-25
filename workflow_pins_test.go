package axiom

import (
	"bufio"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowActionsUseImmutableCommitPins(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no GitHub Actions workflows found")
	}

	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := strings.TrimSpace(scanner.Text())
			if strings.Contains(line, "@latest") {
				t.Errorf("%s:%d uses floating @latest dependency: %s", path, lineNumber, line)
			}
			if !strings.HasPrefix(line, "uses:") && !strings.HasPrefix(line, "- uses:") {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "uses:"))
			if index := strings.IndexByte(value, '#'); index >= 0 {
				value = strings.TrimSpace(value[:index])
			}
			if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "docker://") {
				continue
			}
			at := strings.LastIndexByte(value, '@')
			if at <= 0 || at == len(value)-1 {
				t.Errorf("%s:%d malformed external action reference: %s", path, lineNumber, value)
				continue
			}
			revision := value[at+1:]
			if len(revision) != 40 {
				t.Errorf("%s:%d action is not pinned to a 40-character commit SHA: %s", path, lineNumber, value)
				continue
			}
			if _, err := hex.DecodeString(revision); err != nil {
				t.Errorf("%s:%d action revision is not a hexadecimal commit SHA: %s", path, lineNumber, value)
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
