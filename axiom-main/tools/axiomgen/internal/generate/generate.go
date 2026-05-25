package generate

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"axiom/pkg/axiom"
	"axiom/tools/axiomgen/internal/codegen"
)

type Request struct {
	File        string `json:"file"`
	OutDir      string `json:"out"`
	PackageName string `json:"package"`
}

type FileAction string

const (
	ActionCreate    FileAction = "create"
	ActionOverwrite FileAction = "overwrite"
	ActionSkip      FileAction = "skip"
)

type FilePlan struct {
	Name   string     `json:"name"`
	Path   string     `json:"path"`
	Once   bool       `json:"once"`
	Action FileAction `json:"action"`
}

type Plan struct {
	Domain  string              `json:"domain"`
	Hash    string              `json:"hash"`
	OutDir  string              `json:"out"`
	Package string              `json:"package"`
	Files   []FilePlan          `json:"files"`
	Diff    *codegen.DiffResult `json:"diff,omitempty"`

	files     []codegen.File
	oldSource []byte // previous .axm source extracted from .gen.go
}

type Result struct {
	Domain  string     `json:"domain"`
	Hash    string     `json:"hash"`
	OutDir  string     `json:"out"`
	Package string     `json:"package"`
	Files   []FilePlan `json:"files"`
	Written []string   `json:"written"`
	Skipped []string   `json:"skipped"`
}

func Preview(req Request) (*Plan, error) {
	req = normalize(req)
	if req.File == "" {
		return nil, fmt.Errorf("file is required")
	}
	if req.OutDir == "" {
		return nil, fmt.Errorf("out dir is required")
	}
	if req.PackageName == "" {
		return nil, fmt.Errorf("package name is required")
	}

	source, err := os.ReadFile(req.File)
	if err != nil {
		return nil, err
	}
	module, err := axiom.CompileAny(source, axiom.WithSourceName(req.File))
	if err != nil {
		return nil, err
	}
	files, err := codegen.Generate(module, codegen.Options{
		PackageName: req.PackageName,
		SourcePath:  req.File,
		Source:      source,
	})
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(source)
	plan := &Plan{
		Domain:  module.Domain,
		Hash:    fmt.Sprintf("%x", sum[:]),
		OutDir:  req.OutDir,
		Package: req.PackageName,
		files:   files,
	}
	// If a previous .gen.go exists, extract old source and compute diff.
	if genPath := generatedFilePath(req.OutDir, module.Domain); genPath != "" {
		if oldSrc, err := extractAXMSource(genPath); err == nil && len(oldSrc) > 0 {
			plan.oldSource = oldSrc
			if oldMod, err := axiom.CompileAny(oldSrc, axiom.WithSourceName(req.File)); err == nil {
				src, _ := os.ReadFile(req.File)
				plan.Diff = codegen.Diff(oldSrc, src, oldMod, module)
			}
		}
	}
	for _, file := range files {
		target := filepath.Join(req.OutDir, file.Name)
		action := ActionCreate
		if _, err := os.Stat(target); err == nil {
			if file.Once {
				action = ActionSkip
			} else {
				action = ActionOverwrite
			}
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		plan.Files = append(plan.Files, FilePlan{
			Name:   file.Name,
			Path:   target,
			Once:   file.Once,
			Action: action,
		})
	}
	return plan, nil
}

func Run(req Request) (Result, error) {
	plan, err := Preview(req)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(plan.OutDir, 0o755); err != nil {
		return Result{}, err
	}

	result := Result{
		Domain:  plan.Domain,
		Hash:    plan.Hash,
		OutDir:  plan.OutDir,
		Package: plan.Package,
		Files:   plan.Files,
	}
	for i, file := range plan.files {
		target := plan.Files[i].Path
		if plan.Files[i].Action == ActionSkip {
			// Smart merge: append new activity stubs to existing _activities.go.
			if file.Once {
				merged, err := mergeActivityStubs(target, file.Content)
				if err != nil {
					return Result{}, fmt.Errorf("merge %s: %w", target, err)
				}
				if merged {
					result.Written = append(result.Written, target)
				} else {
					result.Skipped = append(result.Skipped, target)
				}
			} else {
				result.Skipped = append(result.Skipped, target)
			}
			continue
		}
		if err := os.WriteFile(target, file.Content, 0o644); err != nil {
			return Result{}, err
		}
		result.Written = append(result.Written, target)
	}
	return result, nil
}

func DefaultPackageName(outDir string) string {
	outDir = filepath.Clean(outDir)
	base := filepath.Base(outDir)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "generated"
	}
	return sanitizePackageName(base)
}

func normalize(req Request) Request {
	if req.OutDir == "" {
		req.OutDir = "."
		if req.File != "" {
			req.OutDir = filepath.Dir(req.File)
		}
	}
	if req.PackageName == "" {
		req.PackageName = DefaultPackageName(req.OutDir)
	}
	return req
}

func sanitizePackageName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "generated"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || r == '_':
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsDigit(r):
			if b.Len() == 0 {
				b.WriteString("axiom_")
			}
			b.WriteRune(r)
		default:
			if b.Len() == 0 || strings.HasSuffix(b.String(), "_") {
				continue
			}
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" || out == "axiom" {
		if out == "axiom" {
			return out
		}
		return "generated"
	}
	if keyword(out) {
		return out + "_pkg"
	}
	return out
}

func keyword(name string) bool {
	switch name {
	case "break", "default", "func", "interface", "select", "case", "defer", "go", "map", "struct",
		"chan", "else", "goto", "package", "switch", "const", "fallthrough", "if", "range", "type",
		"continue", "for", "import", "return", "var":
		return true
	default:
		return false
	}
}

// ── Diff and merge helpers ──────────────────────────────────────────────────

var axmSourceRx = regexp.MustCompile(`const\s+\w+AXMSource\s*=\s*"(?:[^"\\]|\\.)*"`)

// generatedFilePath returns the path to the .gen.go file for the given domain.
func generatedFilePath(outDir, domain string) string {
	if domain == "" {
		return ""
	}
	base := lowerSnake(domain)
	if base == "" {
		return ""
	}
	return filepath.Join(outDir, base+"_axiom.gen.go")
}

func lowerSnake(name string) string {
	parts := splitName(name)
	for i, part := range parts {
		parts[i] = strings.ToLower(part)
	}
	return strings.Join(parts, "_")
}

func splitName(name string) []string {
	var parts []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			parts = append(parts, string(current))
			current = nil
		}
	}
	var prevLower bool
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			prevLower = false
			continue
		}
		if unicode.IsUpper(r) && prevLower {
			flush()
		}
		current = append(current, r)
		prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	flush()
	return parts
}

// extractAXMSource reads a .gen.go file and extracts the embedded .axm source.
func extractAXMSource(genPath string) ([]byte, error) {
	data, err := os.ReadFile(genPath)
	if err != nil {
		return nil, err
	}
	// Find the raw string literal for *AXMSource.
	// Pattern: const XxxAXMSource = "..."
	match := axmSourceRx.Find(data)
	if match == nil {
		return nil, fmt.Errorf("AXMSource constant not found in %s", genPath)
	}
	// Extract the string literal content.
	eq := strings.Index(string(match), `"`)
	if eq < 0 {
		return nil, fmt.Errorf("malformed AXMSource in %s", genPath)
	}
	// Use strconv.Unquote — but Go raw string literals are backtick-quoted.
	// The generated code uses double-quoted strings with escapes.
	quoted := string(match[eq:])
	unquoted, err := strconvUnquote(quoted)
	if err != nil {
		return nil, fmt.Errorf("unquote AXMSource in %s: %w", genPath, err)
	}
	return []byte(unquoted), nil
}

func strconvUnquote(s string) (string, error) {
	// Simple unquote for Go double-quoted strings.
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("not a quoted string")
	}
	var b strings.Builder
	s = s[1 : len(s)-1]
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteByte(s[i])
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}

// mergeActivityStubs appends new (not yet implemented) activity stubs to an
// existing _activities.go file. Existing methods are preserved untouched.
func mergeActivityStubs(target string, newStubs []byte) (bool, error) {
	existing, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}

	// Parse existing file to find already-implemented methods.
	fset := token.NewFileSet()
	existingAST, err := parser.ParseFile(fset, target, existing, parser.ParseComments)
	if err != nil {
		// If we can't parse the existing file, fall back to full overwrite.
		return false, os.WriteFile(target, newStubs, 0o644)
	}

	existingMethods := collectReceiverMethods(existingAST)

	// Parse new stubs to find all method declarations.
	newAST, err := parser.ParseFile(fset, target, newStubs, 0)
	if err != nil {
		return false, fmt.Errorf("parse generated stubs: %w", err)
	}
	allNewMethods := collectReceiverMethods(newAST)

	// If the existing file has no receiver methods at all, it was hand-edited
	// or is not an axiomgen activity file. Skip merge to avoid appending
	// methods without their receiver type.
	if len(existingMethods) == 0 && len(allNewMethods) > 0 {
		return false, nil
	}

	// Append methods that exist in new but not in existing.
	var toAppend []string
	for name, decl := range allNewMethods {
		if _, ok := existingMethods[name]; !ok {
			src := methodSource(newStubs, decl)
			if src != "" {
				toAppend = append(toAppend, src)
			}
		}
	}
	if len(toAppend) == 0 {
		return false, nil // nothing to add
	}

	// Append new methods at the end of the file.
	f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	for _, method := range toAppend {
		if _, err := fmt.Fprintln(f); err != nil {
			return false, err
		}
		if _, err := fmt.Fprint(f, method); err != nil {
			return false, err
		}
	}
	return true, nil
}

// methodDecl holds the position of a method in its source file.
type methodDecl struct {
	Name string
	Pos  int
	End  int
}

// collectReceiverMethods finds all methods defined on a receiver type
// (e.g., *XxxActivityImpl) and returns them indexed by name.
func collectReceiverMethods(file *ast.File) map[string]methodDecl {
	methods := make(map[string]methodDecl)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		name := fn.Name.Name
		methods[name] = methodDecl{
			Name: name,
			Pos:  int(fn.Pos()),
			End:  int(fn.End()),
		}
	}
	return methods
}

// methodSource extracts the source text of a method declaration from raw bytes.
func methodSource(src []byte, decl methodDecl) string {
	if decl.Pos < 1 || decl.End < 1 || decl.Pos > decl.End || decl.End > len(src) {
		return ""
	}
	// token.FileSet positions are 1-indexed byte offsets.
	return string(src[decl.Pos-1 : decl.End-1])
}
