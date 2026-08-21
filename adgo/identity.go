package adgo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// EncodeDurableName converts any string identifier into an injective, filesystem-safe
// representation. Characters in [a-zA-Z0-9_-] are preserved; all other bytes (including
// separators, '.', '%', and Unicode bytes) are percent-encoded as %XX.
// This guarantees injectivity: EncodeDurableName(a) == EncodeDurableName(b) iff a == b.
// It also ensures that "." and ".." become "%2E" and "%2E%2E", preventing directory traversal.
func EncodeDurableName(value string) string {
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value) * 3)
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return b.String()
}

// DecodeDurableName decodes a string previously encoded with EncodeDurableName.
func DecodeDurableName(encoded string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(encoded); {
		if encoded[i] == '%' {
			if i+2 >= len(encoded) {
				return "", fmt.Errorf("adgo: malformed encoded identifier %q", encoded)
			}
			var bval byte
			n, err := fmt.Sscanf(encoded[i+1:i+3], "%02X", &bval)
			if err != nil || n != 1 {
				return "", fmt.Errorf("adgo: invalid percent hex in identifier %q", encoded)
			}
			b.WriteByte(bval)
			i += 3
		} else {
			b.WriteByte(encoded[i])
			i++
		}
	}
	return b.String(), nil
}

// IsContainedPath verifies that targetPath is strictly inside baseDir and does not escape it.
func IsContainedPath(baseDir, targetPath string) bool {
	cleanBase := filepath.Clean(baseDir)
	cleanTarget := filepath.Clean(targetPath)
	rel, err := filepath.Rel(cleanBase, cleanTarget)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.HasPrefix(rel, "../") {
		return false
	}
	return true
}

// FramedPreimage formats components with explicit length framing: <len1>:<comp1>/<len2>:<comp2>/...
func FramedPreimage(components ...string) string {
	var b strings.Builder
	for i, c := range components {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(fmt.Sprintf("%d:%s", len(c), c))
	}
	return b.String()
}

// CanonicalEventID computes a framed, collision-resistant event identifier.
func CanonicalEventID(e Event) string {
	raw := fmt.Sprintf("%d:%s/%d:%s/%d:%s", len(e.Type), e.Type, len(e.TargetNode), e.TargetNode, len(e.Payload), string(e.Payload))
	sum := sha256.Sum256([]byte(raw))
	return "evt-" + hex.EncodeToString(sum[:8])
}

// CanonicalTaskID computes a framed, collision-resistant task identifier.
func CanonicalTaskID(exec, node string, attempt int) string {
	raw := fmt.Sprintf("%d:%s/%d:%s/%d", len(exec), exec, len(node), node, attempt)
	sum := sha256.Sum256([]byte(raw))
	return "task-" + hex.EncodeToString(sum[:8])
}
