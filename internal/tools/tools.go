package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/benoybose/qodex/internal/model"
)

type Tool struct {
	Name        string
	Description string
	Effect      string
	Parameters  json.RawMessage // JSON Schema for native OpenAI tool calls
	Execute     func(context.Context, json.RawMessage) (Result, error)
}

type Result struct {
	OK       bool                   `json:"ok"`
	Summary  string                 `json:"summary"`
	Content  string                 `json:"content,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type Registry struct {
	root  string
	tools map[string]Tool
	index *ProjectIndex
	mu    sync.Mutex
}

func NewRegistry(projectRoot string) *Registry {
	r := &Registry{root: projectRoot, tools: map[string]Tool{}}
	r.add("list_files", "List files under the project root", "read", listFilesParams, r.listFiles)
	r.add("read_file", "Read a UTF-8 text file", "read", readFileParams, r.readFile)
	r.add("search_text", "Search text in project files", "read", searchTextParams, r.searchText)
	r.add("write_file", "Write a complete file under the project root", "write", writeFileParams, r.writeFile)
	r.add("write_patch", "Apply a unified diff under the project root", "write", writePatchParams, r.writePatch)
	r.add("run_command", "Run a command in the project root", "shell", runCommandParams, r.runCommand)
	r.add("run_script", "Run a pre-approved script from an active skill by description", "shell", runScriptParams, r.runScript)
	r.add("git_status", "Show git status", "read", gitStatusParams, r.gitStatus)
	r.add("git_diff", "Show git diff", "read", gitDiffParams, r.gitDiff)
	r.add("project_index", "Query the project index: list files by language/pattern, find symbols by name/kind, or get a project summary", "read", projectIndexParams, r.projectIndex)
	r.add("run_tests", "Discover and run tests. Accepts pattern (package path or test name regex), file (specific test file), or framework hint (go, pytest, jest, etc.). Returns test output and summary.", "shell", runTestsParams, r.runTests)
	r.add("run_formatter", "Run a code formatter on the project or specific files. Accepts tool (go, ruff, prettier, black, etc.) and path. Requires approval.", "shell", runFormatterParams, r.runFormatter)
	r.add("review_changes", "Analyze uncommitted git changes. Accepts scope (working, staged, or all). Returns structured review of diffs with potential issues and suggestions.", "read", reviewChangesParams, r.reviewChanges)
	r.add("lsp_diagnostics", "Run an LSP language server to get diagnostics (errors, warnings, hints) for a file. Accepts path to a source file. Requires a compatible LSP server (gopls, pyright, etc.) to be installed.", "read", lspDiagnosticsParams, r.lspDiagnostics)
	r.add("lsp_definition", "Go to the definition of a symbol at a given file, line, and column. Accepts path, line (1-based), and character (1-based). Returns the file, line, and column of the definition.", "read", lspDefinitionParams, r.lspDefinition)
	r.add("lsp_find_references", "Find all references to a symbol at a given file, line, and column. Accepts path, line (1-based), and character (1-based). Returns a list of file:line:col locations.", "read", lspFindReferencesParams, r.lspFindReferences)
	return r
}

var defaultParamSchema = json.RawMessage(`{"type":"object"}`)

var (
	listFilesParams = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"max_results":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	readFileParams = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"}},"required":["path"],"additionalProperties":false}`)
	searchTextParams = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"path":{"type":"string"},"case_sensitive":{"type":"boolean"}},"required":["query"],"additionalProperties":false}`)
	writeFileParams = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)
	writePatchParams = json.RawMessage(`{"type":"object","properties":{"patch":{"type":"string"}},"required":["patch"],"additionalProperties":false}`)
	runCommandParams = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"argv":{"type":"array","items":{"type":"string"}},"shell":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	runScriptParams = json.RawMessage(`{"type":"object","properties":{"description":{"type":"string"}},"required":["description"],"additionalProperties":false}`)
	gitStatusParams = json.RawMessage(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`)
	gitDiffParams = json.RawMessage(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`)
	projectIndexParams = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"symbol":{"type":"string"},"kind":{"type":"string"},"lang":{"type":"string"},"max_results":{"type":"integer"},"summary":{"type":"boolean"}},"required":[],"additionalProperties":false}`)
	runTestsParams = json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"file":{"type":"string"},"framework":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	runFormatterParams = json.RawMessage(`{"type":"object","properties":{"tool":{"type":"string"},"path":{"type":"string"}},"required":["tool"],"additionalProperties":false}`)
	reviewChangesParams = json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string"}},"required":[],"additionalProperties":false}`)
	lspDiagnosticsParams = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
	lspDefinitionParams = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"character":{"type":"integer"}},"required":["path","line","character"],"additionalProperties":false}`)
	lspFindReferencesParams = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"character":{"type":"integer"}},"required":["path","line","character"],"additionalProperties":false}`)
)

func (r *Registry) add(name, desc, effect string, parameters json.RawMessage, fn func(context.Context, json.RawMessage) (Result, error)) {
	if parameters == nil {
		parameters = defaultParamSchema
	}
	r.tools[name] = Tool{
		Name: name, Description: desc, Effect: effect,
		Parameters: parameters,
		Execute:    fn,
	}
}

func (r *Registry) ToolSchemas() []model.ToolSchema {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]model.ToolSchema, 0, len(names))
	for _, name := range names {
		t := r.tools[name]
		out = append(out, model.ToolSchema{
			Type: "function",
			Function: model.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) DiffPreview(name string, raw json.RawMessage) (string, error) {
	switch name {
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
		path, err := r.safePath(args.Path)
		if err != nil {
			return "", err
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			existing = []byte{}
		}
		return generateDiff(args.Path, string(existing), args.Content), nil
	case "write_patch":
		var args struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
		if err := validatePatchPaths(args.Patch); err != nil {
			return "", err
		}
		return args.Patch, nil
	default:
		return "", nil
	}
}

type diffEdit struct {
	op   byte
	line string
}

func generateDiff(filename, old, new string) string {
	oldLines := splitLines(old)
	newLines := splitLines(new)

	var b strings.Builder
	b.WriteString("--- a/" + filename + "\n")
	b.WriteString("+++ b/" + filename + "\n")

	if len(oldLines) == 0 && len(newLines) == 0 {
		return b.String()
	}
	if len(oldLines) == 0 {
		b.WriteString("@@ -0,0 +1," + fmt.Sprint(len(newLines)) + " @@\n")
		for _, line := range newLines {
			b.WriteString("+" + line + "\n")
		}
		return b.String()
	}
	if len(newLines) == 0 {
		b.WriteString("@@ -1," + fmt.Sprint(len(oldLines)) + " +0,0 @@\n")
		for _, line := range oldLines {
			b.WriteString("-" + line + "\n")
		}
		return b.String()
	}

	lcs := lcsTable(oldLines, newLines)
	edits := backtrack(oldLines, newLines, lcs)

	// Walk the edit list and emit hunks with 1-line context
	oldPos, newPos := 1, 1
	i := 0
	for i < len(edits) {
		// Skip unchanged lines at start (context before first hunk)
		for i < len(edits) && edits[i].op == ' ' {
			i++
			oldPos++
			newPos++
		}
		if i >= len(edits) {
			break
		}

		// Find the end of this hunk
		hunkStart := i
		ctxBefore := 0
		for hunkStart > 0 && edits[hunkStart-1].op == ' ' && ctxBefore < 1 {
			hunkStart--
			ctxBefore++
		}
		hunkEnd := i
		for hunkEnd < len(edits) && edits[hunkEnd].op != ' ' {
			hunkEnd++
		}
		ctxAfter := 0
		tempEnd := hunkEnd
		for tempEnd < len(edits) && edits[tempEnd].op == ' ' && ctxAfter < 1 {
			tempEnd++
			ctxAfter++
		}

		// Compute hunk position
		hunkOld := oldPos - ctxBefore
		hunkNew := newPos - ctxBefore
		hunkOldLen := 0
		hunkNewLen := 0
		for j := hunkStart; j < tempEnd; j++ {
			switch edits[j].op {
			case '-':
				hunkOldLen++
			case '+':
				hunkNewLen++
			default:
				hunkOldLen++
				hunkNewLen++
			}
		}
		if hunkOldLen == 0 {
			hunkOldLen = 1
		}
		if hunkNewLen == 0 {
			hunkNewLen = 1
		}

		b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunkOld, hunkOldLen, hunkNew, hunkNewLen))
		for j := hunkStart; j < tempEnd; j++ {
			b.WriteByte(edits[j].op)
			b.WriteString(edits[j].line)
			b.WriteByte('\n')
		}

		// Advance position counters past the hunk and its trailing context
		for j := i; j < tempEnd; j++ {
			switch edits[j].op {
			case '-':
				oldPos++
			case '+':
				newPos++
			default:
				oldPos++
				newPos++
			}
		}
		i = tempEnd
	}

	return b.String()
}

func lcsTable(a, b []string) [][]int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

func backtrack(a, b []string, lcs [][]int) []diffEdit {
	var stack []diffEdit
	i, j := len(a), len(b)
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			stack = append(stack, diffEdit{' ', a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			stack = append(stack, diffEdit{'+', b[j-1]})
			j--
		} else if i > 0 {
			stack = append(stack, diffEdit{'-', a[i-1]})
			i--
		}
	}
	edits := make([]diffEdit, len(stack))
	for k := range stack {
		edits[k] = stack[len(stack)-1-k]
	}
	return edits
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (r *Registry) Prompt() string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("Available tools:\n")
	for _, name := range names {
		t := r.tools[name]
		b.WriteString("- ")
		b.WriteString(t.Name)
		b.WriteString(": ")
		b.WriteString(t.Description)
		b.WriteString(" (effect: ")
		b.WriteString(t.Effect)
		b.WriteString(")\n")
	}
	return b.String()
}

func (r *Registry) safePath(path string) (string, error) {
	if path == "" {
		path = "."
	}
	clean := filepath.Clean(path)
	full := filepath.Join(r.root, clean)
	rel, err := filepath.Rel(r.root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(path) {
		return "", fmt.Errorf("path escapes project root: %s", path)
	}
	return full, nil
}

func (r *Registry) listFiles(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		fmt.Fprintf(os.Stderr, "[qodex] list_files: invalid arguments, using defaults: %v\n", err)
	}
	if args.MaxResults <= 0 {
		args.MaxResults = 200
	}
	root, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= args.MaxResults {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(r.root, path)
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OK: true, Summary: fmt.Sprintf("Listed %d files.", len(files)), Content: strings.Join(files, "\n")}, nil
}

func (r *Registry) readFile(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if args.StartLine > 0 || args.EndLine > 0 {
		lines := strings.Split(content, "\n")
		start := max(args.StartLine, 1)
		end := args.EndLine
		if end <= 0 || end > len(lines) {
			end = len(lines)
		}
		if start > end {
			content = ""
		} else {
			content = strings.Join(lines[start-1:end], "\n")
		}
	}
	return Result{OK: true, Summary: fmt.Sprintf("Read %s.", args.Path), Content: truncate(content, 20000)}, nil
}

func (r *Registry) searchText(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Query == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	root, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	var matches []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(matches) >= 200 {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(args.Query)) {
				rel, _ := filepath.Rel(r.root, path)
				matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, i+1, strings.TrimSpace(line)))
				if len(matches) >= 200 {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OK: true, Summary: fmt.Sprintf("Found %d matches.", len(matches)), Content: strings.Join(matches, "\n")}, nil
}

func (r *Registry) writeFile(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o666); err != nil {
		return Result{}, err
	}
	return Result{OK: true, Summary: fmt.Sprintf("Wrote %s.", args.Path)}, nil
}

func (r *Registry) writePatch(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(args.Patch) == "" {
		return Result{}, fmt.Errorf("patch is required")
	}
	if err := validatePatchPaths(args.Patch); err != nil {
		return Result{}, err
	}

	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "apply", "--whitespace=nowarn", "-")
	cmd.Dir = r.root
	cmd.Stdin = strings.NewReader(args.Patch)
	out, err := runWithKillStdin(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Applied unified diff.",
		Content: truncate(string(out), 12000),
	}
	if err != nil {
		res.Summary = "Failed to apply unified diff."
		return res, err
	}
	return res, nil
}

func (r *Registry) runCommand(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Command        string   `json:"command"`
		Argv           []string `json:"argv"`
		Shell          bool     `json:"shell"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if len(args.Argv) == 0 && args.Command == "" {
		return Result{}, fmt.Errorf("command or argv is required")
	}
	if args.TimeoutSeconds <= 0 || args.TimeoutSeconds > 300 {
		args.TimeoutSeconds = 120
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(args.TimeoutSeconds)*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	summary := ""
	if len(args.Argv) > 0 {
		if args.Argv[0] == "" {
			return Result{}, fmt.Errorf("argv[0] is required")
		}
		if err := rejectDangerousArgv(args.Argv); err != nil {
			return Result{}, err
		}
		cmd = exec.CommandContext(cctx, args.Argv[0], args.Argv[1:]...)
		summary = "Ran command: " + strings.Join(args.Argv, " ")
	} else {
		if err := rejectDangerousShellCommand(args.Command); err != nil {
			return Result{}, err
		}
		shell, shellArgs := ShellCommand(args.Command)
		cmd = exec.CommandContext(cctx, shell, shellArgs...)
		args.Shell = true
		summary = "Ran shell command: " + args.Command
	}
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: summary,
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"shell":   args.Shell,
			"network": isNetworkCommand(args.Command, args.Argv),
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) runScript(ctx context.Context, raw json.RawMessage) (Result, error) {
	return Result{OK: false, Summary: "run_script is handled by the agent"}, fmt.Errorf("run_script must be dispatched by the agent")
}

func (r *Registry) gitStatus(ctx context.Context, raw json.RawMessage) (Result, error) {
	return r.git(ctx, "status --short")
}

func (r *Registry) gitDiff(ctx context.Context, raw json.RawMessage) (Result, error) {
	return r.git(ctx, "diff")
}

func (r *Registry) git(ctx context.Context, command string) (Result, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	parts := strings.Fields(command)
	cmd := exec.CommandContext(cctx, "git", parts...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{OK: err == nil, Summary: "Ran git " + command, Content: truncate(string(out), 20000)}
	if err != nil {
		return res, err
	}
	return res, nil
}

func rejectDangerousShellCommand(command string) error {
	normalized := strings.Join(strings.Fields(strings.ToLower(command)), " ")
	dangerous := []string{
		"rm -rf /",
		"rm -fr /",
		"rm -rf ~",
		"rm -fr ~",
		"rm -rf $home",
		"rm -fr $home",
		"rm -rf %userprofile%",
		"rm -fr %userprofile%",
		"sudo rm -rf",
		"sudo rm -fr",
		"mkfs",
		"diskutil erase",
		":(){ :|:& };:",
		"curl |",
		"wget |",
		"eval ",
		"remove-item -recurse -force c:\\",
		"remove-item -rec -force c:\\",
	}
	for _, pattern := range dangerous {
		if strings.Contains(normalized, pattern) {
			return fmt.Errorf("refusing dangerous shell command: %s", pattern)
		}
	}
	return nil
}

func isRootPath(p string) bool {
	if p == "/" {
		return true
	}
	if filepath.IsAbs(p) {
		cleaned := filepath.Clean(p)
		if cleaned == string(filepath.Separator) {
			return true
		}
		if vol := filepath.VolumeName(cleaned); vol != "" {
			return cleaned == vol+string(filepath.Separator)
		}
	}
	return false
}

func rejectDangerousArgv(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	if base == "rm" {
		hasNoPreserve := false
		for _, arg := range argv[1:] {
			if isRootPath(arg) {
				return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
			}
			if strings.HasPrefix(arg, "/") && (strings.Contains(arg, "*") || strings.Contains(arg, "..")) {
				return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
			}
			if filepath.IsAbs(arg) && (strings.Contains(arg, "*") || strings.Contains(arg, "..")) {
				return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
			}
			if arg == "--no-preserve-root" {
				hasNoPreserve = true
			}
			if strings.Contains(arg, "rf") && strings.HasPrefix(arg, "-") {
				for _, target := range argv[1:] {
					if isRootPath(target) {
						return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
					}
				}
			}
		}
		if hasNoPreserve {
			for _, arg := range argv[1:] {
				if isRootPath(arg) {
					return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
				}
			}
		}
	}
	if base == "mkfs" || strings.HasPrefix(base, "mkfs.") {
		return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
	}
	if base == "diskutil" && len(argv) > 1 && strings.EqualFold(argv[1], "erase") {
		return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
	}
	return nil
}

func IsNetworkCommand(raw json.RawMessage) bool {
	var args struct {
		Command string   `json:"command"`
		Argv    []string `json:"argv"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return false
	}
	return isNetworkCommand(args.Command, args.Argv)
}

func isNetworkCommand(command string, argv []string) bool {
	if len(argv) > 0 {
		return isNetworkArgv(argv)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(command)), " ")
	networkPatterns := []string{
		"curl ", "wget ", "git clone", "git pull", "git fetch", "go get", "go install",
		"npm install", "npm i", "pnpm install", "yarn install", "pip install",
		"cargo install", "brew install", "apt install", "apt-get install",
		"dnf install", "yum install", "apk add", "ssh ", "scp ", "rsync ",
	}
	for _, pattern := range networkPatterns {
		if strings.Contains(normalized, pattern) || strings.HasPrefix(normalized, strings.TrimSpace(pattern)) {
			return true
		}
	}
	return false
}

func isNetworkArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	switch base {
	case "curl", "wget", "ssh", "scp", "rsync":
		return true
	case "git":
		return len(argv) > 1 && (argv[1] == "clone" || argv[1] == "pull" || argv[1] == "fetch")
	case "go":
		return len(argv) > 1 && (argv[1] == "get" || argv[1] == "install")
	case "npm":
		return len(argv) > 1 && (argv[1] == "install" || argv[1] == "i")
	case "pnpm", "yarn", "pip", "cargo", "brew":
		return len(argv) > 1 && argv[1] == "install"
	case "apt", "apt-get", "dnf", "yum":
		return len(argv) > 1 && argv[1] == "install"
	case "apk":
		return len(argv) > 1 && argv[1] == "add"
	default:
		return false
	}
}

func runWithKill(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- result{out: out, err: err}
	}()
	select {
	case r := <-done:
		return r.out, r.err
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return nil, ctx.Err()
	}
}

func runWithKillStdin(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- result{out: out, err: err}
	}()
	select {
	case r := <-done:
		return r.out, r.err
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return nil, ctx.Err()
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for !utf8.ValidString(s[:cut]) && cut > 0 {
		cut--
	}
	return s[:cut] + "\n... truncated ..."
}

func (r *Registry) runTests(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Pattern   string `json:"pattern"`
		File      string `json:"file"`
		Framework string `json:"framework"`
		Timeout   int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Pattern == "" && args.File == "" {
		args.Pattern = "./..."
	}
	if args.Timeout <= 0 || args.Timeout > 600 {
		args.Timeout = 300
	}

	if args.Framework == "" {
		if args.File != "" {
			args.Framework = detectFramework(filepath.Ext(args.File))
		} else {
			args.Framework = detectProjectFramework(r.root)
		}
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(args.Timeout)*time.Second)
	defer cancel()

	summary := ""
	var content string

		switch args.Framework {
		case "go":
			argv := []string{"go", "test"}
			if args.Pattern != "" {
				argv = append(argv, args.Pattern)
			}
			if args.File != "" {
				argv = append(argv, args.File)
			}
			cmd := exec.CommandContext(cctx, "go", argv[1:]...)
			cmd.Dir = r.root
			out, err := runWithKill(cctx, cmd)
			summary = fmt.Sprintf("Ran go test %s", args.Pattern)
			content = string(out)
			if err != nil {
				return Result{OK: false, Summary: "Tests failed", Content: truncate(content, 20000)}, err
			}
			return Result{OK: true, Summary: summary, Content: truncate(content, 20000), Metadata: map[string]interface{}{"shell": false, "network": false}}, nil

		case "pytest", "python":
			argv := []string{"python", "-m", "pytest"}
			if args.Pattern != "" {
				argv = append(argv, args.Pattern)
			}
			if args.File != "" {
				argv = append(argv, args.File)
			}
			cmd := exec.CommandContext(cctx, "python", argv[1:]...)
			cmd.Dir = r.root
			out, err := runWithKill(cctx, cmd)
			summary = fmt.Sprintf("Ran pytest %s", args.Pattern)
			content = string(out)
			if err != nil {
				return Result{OK: false, Summary: "Tests failed", Content: truncate(content, 20000)}, err
			}
			return Result{OK: true, Summary: summary, Content: truncate(content, 20000), Metadata: map[string]interface{}{"shell": false, "network": false}}, nil

	case "jest", "node":
		if hasFile(r.root, "package.json") {
				argv := []string{"npx", "jest"}
				if args.Pattern != "" {
					argv = append(argv, args.Pattern)
				}
				if args.File != "" {
					argv = append(argv, args.File)
				}
				cmd := exec.CommandContext(cctx, "npx", argv[1:]...)
				cmd.Dir = r.root
				out, err := runWithKill(cctx, cmd)
				summary = fmt.Sprintf("Ran jest %s", args.Pattern)
				content = string(out)
			if err != nil {
				return Result{OK: false, Summary: "Tests failed", Content: truncate(content, 20000)}, err
			}
			return Result{OK: true, Summary: summary, Content: truncate(content, 20000), Metadata: map[string]interface{}{"shell": false, "network": false}}, nil
		}

	default:
		if args.Framework == "" {
			return Result{}, fmt.Errorf("no supported test runner detected; try go, pytest, or jest")
		}
		return Result{}, fmt.Errorf("unsupported test framework %q; try go, pytest, or jest", args.Framework)
	}
	return Result{}, fmt.Errorf("unreachable: all framework cases return")
}

func hasFile(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

func detectFramework(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "pytest"
	case ".js", ".jsx", ".ts", ".tsx":
		return "jest"
	default:
		return ""
	}
}

func detectProjectFramework(root string) string {
	if hasFile(root, "go.mod") {
		return "go"
	}
	if hasFile(root, "pyproject.toml") || hasFile(root, "setup.py") || hasFile(root, "requirements.txt") {
		return "pytest"
	}
	if hasFile(root, "package.json") || hasFile(root, "jest.config.js") || hasFile(root, "jest.config.ts") {
		return "jest"
	}
	return ""
}

func (r *Registry) runFormatter(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Tool string `json:"tool"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Tool == "" {
		return Result{}, fmt.Errorf("tool is required (e.g. go, ruff, prettier, black)")
	}

	workPath := r.root
	if args.Path != "" {
		var err error
		workPath, err = r.safePath(args.Path)
		if err != nil {
			return Result{}, err
		}
	}

	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	summary := ""
	switch strings.ToLower(args.Tool) {
	case "go", "gofmt":
		argv := []string{"go", "fmt"}
		if args.Path != "" {
			dir := filepath.Dir(args.Path)
			if dir != "." {
				argv = append(argv, "./"+dir)
			} else {
				argv = append(argv, "./"+filepath.Base(args.Path))
			}
		} else {
			argv = append(argv, "./...")
		}
		// go fmt writes to a relative path; we must run it from the root
		cmd = exec.CommandContext(cctx, "go", "fmt", argv[2])
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran go fmt on %s", args.Path)
		if args.Path == "" {
			summary = "Ran go fmt on project"
		}

	case "ruff":
		argv := []string{"ruff", "format"}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, "ruff", argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran ruff format on %s", workPath)

	case "black":
		argv := []string{"black"}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, "black", argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran black on %s", workPath)

	case "prettier":
		argv := []string{"npx", "prettier", "--write"}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, "npx", argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran prettier on %s", workPath)

	default:
		// Try running the tool directly with the path as argument
		argv := []string{args.Tool}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, args.Tool, argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran %s on %s", args.Tool, workPath)
	}

	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: summary,
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"shell":   false,
			"network": false,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) reviewChanges(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		fmt.Fprintf(os.Stderr, "[qodex] review_changes: invalid arguments, using defaults: %v\n", err)
	}
	if args.Scope == "" {
		args.Scope = "all"
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var b strings.Builder
	b.WriteString("# Review of Uncommitted Changes\n\n")

	// Get git diff
	diffArgs := []string{"diff"}
	if args.Scope == "staged" {
		diffArgs = []string{"diff", "--staged"}
	} else if args.Scope == "working" {
		diffArgs = []string{"diff"}
	} else {
		// all: staged + working
		diffArgs = []string{"diff", "HEAD"}  // includes both staged and unstaged
	}

	cmd := exec.CommandContext(cctx, "git", diffArgs...)
	cmd.Dir = r.root
	diffOut, err := runWithKill(cctx, cmd)
	if err != nil {
		return Result{}, fmt.Errorf("git diff failed: %w", err)
	}
	diffText := string(diffOut)
	if strings.TrimSpace(diffText) == "" {
		return Result{OK: true, Summary: "No changes to review", Content: "No uncommitted changes found."}, nil
	}

	cmd2 := exec.CommandContext(cctx, "git", "diff", "--name-status")
	if args.Scope == "staged" {
		cmd2 = exec.CommandContext(cctx, "git", "diff", "--staged", "--name-status")
	} else if args.Scope == "working" {
		cmd2 = exec.CommandContext(cctx, "git", "diff", "--name-status")
	} else {
		cmd2 = exec.CommandContext(cctx, "git", "diff", "HEAD", "--name-status")
	}
	cmd2.Dir = r.root
	statusOut, _ := runWithKill(cctx, cmd2)

	cmd3 := exec.CommandContext(cctx, "git", "status", "--short")
	cmd3.Dir = r.root
	statusShort, _ := runWithKill(cctx, cmd3)

	b.WriteString("## Changed Files\n\n")
	b.WriteString("```\n")
	b.WriteString(string(statusOut))
	b.WriteString("```\n\n")

	b.WriteString("## Untracked Files\n\n")
	untracked := extractUntracked(string(statusShort))
	if len(untracked) > 0 {
		for _, u := range untracked {
			b.WriteString(fmt.Sprintf("- %s (untracked)\n", u))
		}
	} else {
		b.WriteString("No untracked files.\n")
	}
	b.WriteString("\n")
	b.WriteString("## Diff Summary\n\n")
	b.WriteString(fmt.Sprintf("Total diff size: %d bytes across changed files.\n", len(diffText)))
	b.WriteString("\n## Full Diff\n\n```diff\n")
	b.WriteString(diffText)
	b.WriteString("```")

	content := b.String()
	summary := fmt.Sprintf("Reviewing %d bytes of changes", len(diffText))
	if len(content) > 25000 {
		content = content[:25000] + "\n... truncated ..."
	}

	return Result{
		OK:      true,
		Summary: summary,
		Content: truncate(content, 50000),
		Metadata: map[string]interface{}{
			"diff_size": len(diffText),
			"scope":     args.Scope,
		},
	}, nil
}

func extractUntracked(gitStatus string) []string {
	var out []string
	for _, line := range strings.Split(gitStatus, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "?? ") {
			out = append(out, strings.TrimPrefix(line, "?? "))
		}
	}
	return out
}

func validatePatchPaths(patch string) error {
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[1] == "/dev/null" {
				continue
			}
			path := strings.TrimPrefix(fields[1], "a/")
			path = strings.TrimPrefix(path, "b/")
			if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(filepath.Clean(path), "../") {
				return fmt.Errorf("patch path escapes project root: %s", fields[1])
			}
		}
	}
	return nil
}
