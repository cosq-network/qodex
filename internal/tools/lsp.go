package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/benoybose/qodex/internal/lsp"
)

type lspServerCmd struct {
	cmd  string
	args []string
}

var lspServers = map[string]lspServerCmd{
	".go":  {"gopls", nil},
	".py":  {"pyright-langserver", []string{"--stdio"}},
	".js":  {"typescript-language-server", []string{"--stdio"}},
	".jsx": {"typescript-language-server", []string{"--stdio"}},
	".mjs": {"typescript-language-server", []string{"--stdio"}},
	".ts":  {"typescript-language-server", []string{"--stdio"}},
	".tsx": {"typescript-language-server", []string{"--stdio"}},
	".rs":  {"rust-analyzer", nil},
}

func lspCommandFor(path string) (lspServerCmd, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	sc, ok := lspServers[ext]
	return sc, ok
}

func lspServerHelp(ext string) string {
	switch ext {
	case ".go":
		return "Install gopls: go install golang.org/x/tools/gopls@latest"
	case ".py":
		return "Install pyright: pip install pyright or npm install -g pyright"
	case ".js", ".jsx", ".mjs", ".ts", ".tsx":
		return "Install typescript-language-server: npm install -g typescript-language-server"
	case ".rs":
		return "Install rust-analyzer: rustup component add rust-analyzer"
	default:
		return "No LSP server configured for " + ext
	}
}

func checkServerAvailable(sc lspServerCmd) error {
	_, err := exec.LookPath(sc.cmd)
	return err
}

func startLSP(ctx context.Context, sc lspServerCmd, root, relPath string) (*lsp.Client, error) {
	client, err := lsp.NewClient(ctx, sc.cmd, sc.args)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", sc.cmd, err)
	}
	if err := client.Initialize(root); err != nil {
		client.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if err := client.OpenDocument(relPath); err != nil {
		client.Close()
		return nil, fmt.Errorf("open document: %w", err)
	}
	return client, nil
}

type lspDiagnosticsArgs struct {
	Path string `json:"path"`
}

func (r *Registry) lspDiagnostics(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args lspDiagnosticsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	fullPath, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}

	sc, ok := lspCommandFor(args.Path)
	if !ok {
		return Result{}, fmt.Errorf("unsupported file type: %s\n%s", filepath.Ext(args.Path), lspServerHelp(filepath.Ext(args.Path)))
	}
	if err := checkServerAvailable(sc); err != nil {
		return Result{}, fmt.Errorf("LSP server %q not found: %v\n%s", sc.cmd, err, lspServerHelp(filepath.Ext(args.Path)))
	}

	// Read the file to ensure it exists
	if _, err := os.Stat(fullPath); err != nil {
		return Result{}, fmt.Errorf("file not found: %s", args.Path)
	}

	client, err := startLSP(ctx, sc, r.root, args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("LSP: %w", err)
	}
	defer client.Close()

	diags, err := client.Diagnostics(args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("diagnostics: %w", err)
	}

	if len(diags) == 0 {
		return Result{
			OK:      true,
			Summary: fmt.Sprintf("No diagnostics for %s", args.Path),
			Content: "No issues found.",
		}, nil
	}

	severityLabel := map[int]string{
		1: "error",
		2: "warning",
		3: "info",
		4: "hint",
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Diagnostics for %s (%d issues):\n", args.Path, len(diags)))
	for _, d := range diags {
		label := severityLabel[d.Severity]
		if label == "" {
			label = "unknown"
		}
		b.WriteString(fmt.Sprintf("  %s:%d:%d: [%s] %s",
			args.Path, d.Range.Start.Line+1, d.Range.Start.Character+1, label, d.Message))
		if d.Source != "" {
			b.WriteString(fmt.Sprintf(" (%s)", d.Source))
		}
		b.WriteString("\n")
	}

	return Result{
		OK:      true,
		Summary: fmt.Sprintf("Found %d diagnostic(s) for %s", len(diags), args.Path),
		Content: b.String(),
	}, nil
}

type lspDefinitionArgs struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

func (r *Registry) lspDefinition(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args lspDefinitionArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	fullPath, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}

	sc, ok := lspCommandFor(args.Path)
	if !ok {
		return Result{}, fmt.Errorf("unsupported file type: %s\n%s", filepath.Ext(args.Path), lspServerHelp(filepath.Ext(args.Path)))
	}
	if err := checkServerAvailable(sc); err != nil {
		return Result{}, fmt.Errorf("LSP server %q not found: %v\n%s", sc.cmd, err, lspServerHelp(filepath.Ext(args.Path)))
	}

	if _, err := os.Stat(fullPath); err != nil {
		return Result{}, fmt.Errorf("file not found: %s", args.Path)
	}

	client, err := startLSP(ctx, sc, r.root, args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("LSP: %w", err)
	}
	defer client.Close()

	loc, err := client.GotoDefinition(args.Path, args.Line-1, args.Character-1)
	if err != nil {
		return Result{}, fmt.Errorf("definition: %w", err)
	}

	relFile := loc.URI
	if strings.HasPrefix(loc.URI, "file://") {
		relFile = strings.TrimPrefix(loc.URI, "file://")
		if rootRel, err := filepath.Rel(r.root, relFile); err == nil {
			relFile = rootRel
		}
		relFile = filepath.ToSlash(relFile)
		for strings.HasPrefix(relFile, "/") {
			relFile = strings.TrimPrefix(relFile, "/")
		}
	}
	clean := filepath.Clean(relFile)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return Result{}, fmt.Errorf("LSP result path escapes project root: %s", relFile)
	}

	return Result{
		OK:      true,
		Summary: fmt.Sprintf("Definition of symbol at %s:%d:%d", args.Path, args.Line, args.Character),
		Content: fmt.Sprintf("%s:%d:%d", relFile, loc.Range.Start.Line+1, loc.Range.Start.Character+1),
		Metadata: map[string]interface{}{
			"file":  relFile,
			"line":  loc.Range.Start.Line + 1,
			"col":   loc.Range.Start.Character + 1,
			"start": map[string]int{"line": loc.Range.Start.Line + 1, "character": loc.Range.Start.Character + 1},
			"end":   map[string]int{"line": loc.Range.End.Line + 1, "character": loc.Range.End.Character + 1},
		},
	}, nil
}

type lspFindReferencesArgs struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

func (r *Registry) lspFindReferences(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args lspFindReferencesArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	fullPath, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}

	sc, ok := lspCommandFor(args.Path)
	if !ok {
		return Result{}, fmt.Errorf("unsupported file type: %s\n%s", filepath.Ext(args.Path), lspServerHelp(filepath.Ext(args.Path)))
	}
	if err := checkServerAvailable(sc); err != nil {
		return Result{}, fmt.Errorf("LSP server %q not found: %v\n%s", sc.cmd, err, lspServerHelp(filepath.Ext(args.Path)))
	}

	if _, err := os.Stat(fullPath); err != nil {
		return Result{}, fmt.Errorf("file not found: %s", args.Path)
	}

	client, err := startLSP(ctx, sc, r.root, args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("LSP: %w", err)
	}
	defer client.Close()

	refs, err := client.FindReferences(args.Path, args.Line-1, args.Character-1)
	if err != nil {
		return Result{}, fmt.Errorf("references: %w", err)
	}

	if len(refs) == 0 {
		return Result{
			OK:      true,
			Summary: fmt.Sprintf("No references found at %s:%d:%d", args.Path, args.Line, args.Character),
			Content: "No references found.",
		}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("References at %s:%d:%d (%d total):\n", args.Path, args.Line, args.Character, len(refs)))
	locations := make([]map[string]interface{}, 0, len(refs))

	for _, ref := range refs {
		relFile := ref.URI
		if strings.HasPrefix(ref.URI, "file://") {
			relFile = strings.TrimPrefix(ref.URI, "file://")
			if rootRel, err := filepath.Rel(r.root, relFile); err == nil {
				relFile = rootRel
			}
			relFile = filepath.ToSlash(relFile)
			for strings.HasPrefix(relFile, "/") {
				relFile = strings.TrimPrefix(relFile, "/")
			}
		}
		clean := filepath.Clean(relFile)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return Result{}, fmt.Errorf("LSP result path escapes project root: %s", relFile)
		}
		b.WriteString(fmt.Sprintf("  %s:%d:%d\n", relFile, ref.Range.Start.Line+1, ref.Range.Start.Character+1))
		locations = append(locations, map[string]interface{}{
			"file": relFile,
			"line": ref.Range.Start.Line + 1,
			"col":  ref.Range.Start.Character + 1,
		})
	}

	return Result{
		OK:       true,
		Summary:  fmt.Sprintf("Found %d reference(s)", len(refs)),
		Content:  b.String(),
		Metadata: map[string]interface{}{"locations": locations},
	}, nil
}
