package model

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const ggufMagic = "GGUF"

type ProgressFunc func(downloaded, total int64)

type ModelRegistry struct {
	installRoot string
	progress    ProgressFunc
}

func NewModelRegistry(installRoot string) *ModelRegistry {
	return &ModelRegistry{installRoot: installRoot}
}

func (r *ModelRegistry) SetProgressFunc(fn ProgressFunc) {
	r.progress = fn
}

func (r *ModelRegistry) ModelsDir() string {
	return filepath.Join(r.installRoot, "models")
}

func (r *ModelRegistry) IsDownloaded(modelName string) bool {
	if modelName == "" {
		return false
	}
	candidates := []string{
		filepath.Join(r.ModelsDir(), modelName),
		filepath.Join(r.ModelsDir(), modelName+".gguf"),
		filepath.Join(r.ModelsDir(), modelName+".bin"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

type ModelInfo struct {
	Name       string
	Size       string
	URL        string
	Downloaded bool
}

var defaultModels = []ModelInfo{
	{Name: "qwen2.5-coder-7b-q4_k_m.gguf", Size: "4.7 GB", URL: "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-instruct-q4_k_m.gguf"},
	{Name: "qwen2.5-coder-7b-fp16.gguf", Size: "14 GB", URL: "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-instruct-fp16.gguf"},
	{Name: "qwen2.5-coder-32b-q4_k_m.gguf", Size: "20 GB", URL: "https://huggingface.co/Qwen/Qwen2.5-Coder-32B-Instruct-GGUF/resolve/main/qwen2.5-coder-32b-instruct-q4_k_m.gguf"},
	{Name: "deepseek-coder-6.7b-q4_k_m.gguf", Size: "3.8 GB", URL: "https://huggingface.co/mradermacher/deepseek-coder-6.7b-instruct-GGUF/resolve/main/deepseek-coder-6.7b-instruct.Q4_K_M.gguf"},
}

func (r *ModelRegistry) List() ([]ModelInfo, error) {
	entries, err := os.ReadDir(r.ModelsDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	installedMap := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".gguf") {
			installedMap[entry.Name()] = true
		}
	}

	var result []ModelInfo
	for _, m := range defaultModels {
		result = append(result, ModelInfo{
			Name:       m.Name,
			Size:       m.Size,
			URL:        m.URL,
			Downloaded: installedMap[m.Name],
		})
	}

	return result, nil
}

func (r *ModelRegistry) Download(ctx context.Context, modelName string) error {
	modelURL := ""
	for _, m := range defaultModels {
		if m.Name == modelName {
			modelURL = m.URL
			break
		}
	}

	if modelURL == "" {
		fmt.Printf("Manual download required for %s\n", modelName)
		fmt.Printf("Place the model file in: %s\n", r.ModelsDir())
		return fmt.Errorf("no automatic download - see: https://huggingface.co/models?library=gguf for GGUF models")
	}

	if err := os.MkdirAll(r.ModelsDir(), 0o755); err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}

	dest := filepath.Join(r.ModelsDir(), modelName)
	offset := int64(0)
	if fi, err := os.Stat(dest); err == nil {
		offset = fi.Size()
	}

	if offset > 0 {
		remoteSize, err := r.remoteSize(ctx, modelURL)
		switch {
		case err != nil || remoteSize < 0:
			// Remote size unknown; the GET below will tell us whether a
			// Range restart is possible.
		case offset == remoteSize:
			if VerifyGGUF(dest) == nil {
				fmt.Println("Model already downloaded")
				return nil
			}
			fmt.Printf("Existing file is corrupt; re-downloading %s\n", modelName)
			if err := os.Remove(dest); err != nil {
				return err
			}
			offset = 0
		case offset > remoteSize:
			fmt.Printf("Existing file is larger than remote; re-downloading %s\n", modelName)
			if err := os.Remove(dest); err != nil {
				return err
			}
			offset = 0
		default:
			fmt.Printf("Existing file is incomplete; resuming download of %s\n", modelName)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "qodex-setup")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// Server does not support Range (or this is a fresh download).
		offset = 0
	case http.StatusPartialContent:
		// Resuming from the partial file.
	case http.StatusRequestedRangeNotSatisfiable:
		if VerifyGGUF(dest) == nil {
			fmt.Printf("Model %s is already complete\n", modelName)
			return nil
		}
		return fmt.Errorf("existing file is incomplete or corrupt; remove %s and retry", dest)
	default:
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total >= 0 && offset > 0 {
		total += offset
	}

	var dst *os.File
	if offset > 0 {
		dst, err = os.OpenFile(dest, os.O_WRONLY|os.O_APPEND, 0o666)
	} else {
		dst, err = os.Create(dest)
	}
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	fmt.Printf("Downloading %s...\n", modelName)
	buf := make([]byte, 256*1024)
	var writer *progressWriter
	writer = &progressWriter{
		w: dst,
		onWrite: func(n int64) {
			if r.progress != nil {
				r.progress(offset+writer.total, total)
			}
		},
	}
	if _, err := io.CopyBuffer(writer, resp.Body, buf); err != nil {
		_ = dst.Close()
		return fmt.Errorf("save failed: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close downloaded file: %w", err)
	}

	if err := VerifyGGUF(dest); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("downloaded file is not a valid GGUF model: %w", err)
	}
	if r.progress != nil {
		fmt.Println()
	}
	fmt.Printf("Downloaded to %s\n", dest)
	return nil
}

// VerifyGGUF checks that path contains a valid GGUF model file by inspecting
// the magic header ("GGUF") in the first four bytes.
func VerifyGGUF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("unable to read header: %w", err)
	}
	if string(magic[:]) != ggufMagic {
		return fmt.Errorf("invalid magic %q, expected %q", magic[:], ggufMagic)
	}
	return nil
}

// remoteSize returns the size of the remote model file in bytes, or -1 when
// the server does not advertise one (e.g. HEAD unsupported or chunked).
func (r *ModelRegistry) remoteSize(ctx context.Context, modelURL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, modelURL, nil)
	if err != nil {
		return -1, err
	}
	req.Header.Set("User-Agent", "qodex-setup")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return -1, nil
	}
	return resp.ContentLength, nil
}

type progressWriter struct {
	w       io.Writer
	total   int64
	onWrite func(int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 {
		p.total += int64(n)
		if p.onWrite != nil {
			p.onWrite(int64(n))
		}
	}
	return n, err
}
