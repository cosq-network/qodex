package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type FileEntry struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Size     int64  `json:"size"`
}

type SymbolEntry struct {
	File     string `json:"file"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Line     int    `json:"line"`
	Language string `json:"language"`
}

type ProjectIndex struct {
	files   []FileEntry
	symbols []SymbolEntry
}

var funcRe = map[string]*regexp.Regexp{
	"go":  regexp.MustCompile(`^(func|type|struct|interface)\s+(\w+)`),
	"py":  regexp.MustCompile(`^(def|class)\s+(\w+)`),
	"js":  regexp.MustCompile(`^(function|class|const|let|var)\s+(\w+)`),
	"ts":  regexp.MustCompile(`^(function|class|const|let|var|interface|type|enum)\s+(\w+)`),
	"rs":  regexp.MustCompile(`^(fn|struct|enum|trait|impl|type|const)\s+(\w+)`),
	"java": regexp.MustCompile(`^\s*(public|private|protected)?\s*(class|interface|enum|record)\s+(\w+)`),
}

func langFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc", ".cxx":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".toml", ".yaml", ".yml", ".json", ".md":
		return "config"
	default:
		return "other"
	}
}

func NewProjectIndex(root string) *ProjectIndex {
	idx := &ProjectIndex{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == ".qodex" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		info, _ := d.Info()
		fe := FileEntry{
			Path:     rel,
			Language: langFromExt(rel),
			Size:     info.Size(),
		}
		idx.files = append(idx.files, fe)

		lang := strings.ToLower(filepath.Ext(rel))
		re := funcRe[strings.TrimPrefix(lang, ".")]
		if re == nil {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			matches := re.FindStringSubmatch(strings.TrimSpace(line))
			if matches != nil {
				kind := "func"
				nameIdx := 1
				for i, m := range matches[1:] {
					if m == "func" || m == "fn" || m == "function" || m == "def" {
						kind = "function"
						nameIdx = i + 2
						break
					}
					if m == "type" || m == "struct" || m == "interface" || m == "trait" || m == "enum" || m == "class" || m == "record" {
						kind = m
						nameIdx = i + 2
						break
					}
					if m == "const" || m == "let" || m == "var" {
						kind = "variable"
						nameIdx = i + 2
						break
					}
				}
				if nameIdx < len(matches) {
					idx.symbols = append(idx.symbols, SymbolEntry{
						File:     rel,
						Name:     matches[nameIdx],
						Kind:     kind,
						Line:     lineNum,
						Language: fe.Language,
					})
				}
			}
		}
		return nil
	})
	return idx
}

func (idx *ProjectIndex) QueryFiles(lang, pattern string, maxResults int) []FileEntry {
	if maxResults <= 0 {
		maxResults = 200
	}
	pat := strings.ToLower(pattern)
	var out []FileEntry
	for _, f := range idx.files {
		if len(out) >= maxResults {
			break
		}
		if lang != "" && f.Language != lang {
			continue
		}
		if pattern != "" && !strings.Contains(strings.ToLower(f.Path), pat) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func (idx *ProjectIndex) QuerySymbols(name, kind, lang string, maxResults int) []SymbolEntry {
	if maxResults <= 0 {
		maxResults = 100
	}
	needle := strings.ToLower(name)
	var out []SymbolEntry
	for _, s := range idx.symbols {
		if len(out) >= maxResults {
			break
		}
		if name != "" && !strings.Contains(strings.ToLower(s.Name), needle) {
			continue
		}
		if kind != "" && s.Kind != kind {
			continue
		}
		if lang != "" && s.Language != lang {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (idx *ProjectIndex) Summary() string {
	langs := map[string]int{}
	totalSize := int64(0)
	for _, f := range idx.files {
		langs[f.Language]++
		totalSize += f.Size
	}
	keys := make([]string, 0, len(langs))
	for k := range langs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Project index: %d files, %d symbols, %.1f KB total\n", len(idx.files), len(idx.symbols), float64(totalSize)/1024))
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("- %s: %d files\n", k, langs[k]))
	}
	return b.String()
}

func (r *Registry) projectIndex(ctx context.Context, raw json.RawMessage) (Result, error) {
	if r.index == nil {
		r.index = NewProjectIndex(r.root)
	}
	var args struct {
		Query       string `json:"query"`
		Symbol      string `json:"symbol"`
		Kind        string `json:"kind"`
		Lang        string `json:"lang"`
		MaxResults  int    `json:"max_results"`
		Summary bool `json:"summary"`
	}
	_ = json.Unmarshal(raw, &args)

	if args.Summary {
		return Result{OK: true, Summary: "Project index summary", Content: r.index.Summary()}, nil
	}

	if args.Symbol != "" {
		symbols := r.index.QuerySymbols(args.Symbol, args.Kind, args.Lang, args.MaxResults)
		if len(symbols) == 0 {
			return Result{OK: true, Summary: "No symbols found", Content: "No matching symbols found in the project index."}, nil
		}
		var b strings.Builder
		for _, s := range symbols {
			b.WriteString(fmt.Sprintf("%s:%d:%s (%s) [%s]\n", s.File, s.Line, s.Name, s.Kind, s.Language))
		}
		return Result{
			OK:      true,
			Summary: fmt.Sprintf("Found %d symbols matching %q.", len(symbols), args.Symbol),
			Content: b.String(),
		}, nil
	}

	files := r.index.QueryFiles(args.Lang, args.Query, args.MaxResults)
	if len(files) == 0 {
		return Result{OK: true, Summary: "No files found", Content: "No matching files found in the project index."}, nil
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return Result{
		OK:      true,
		Summary: fmt.Sprintf("Found %d files.", len(files)),
		Content: strings.Join(paths, "\n"),
	}, nil
}
