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

type ModelRegistry struct {
	installRoot string
}

func NewModelRegistry(installRoot string) *ModelRegistry {
	return &ModelRegistry{installRoot: installRoot}
}

func (r *ModelRegistry) ModelsDir() string {
	return filepath.Join(r.installRoot, "models")
}

type ModelInfo struct {
	Name       string
	Size       string
	URL        string
	Downloaded bool
}

var defaultModels = []ModelInfo{
	{Name: "qwen2.5-coder-7b-q4_k_m.gguf", Size: "4.7 GB", URL: "https://huggingface.co/TheBloke/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-instruct-q4_k_m.gguf"},
	{Name: "qwen2.5-coder-7b-fp16.gguf", Size: "14 GB", URL: "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-instruct-fp16.gguf"},
	{Name: "qwen2.5-coder-32b-q4_k_m.gguf", Size: "20 GB", URL: "https://huggingface.co/TheBloke/Qwen2.5-Coder-32B-Instruct-GGUF/resolve/main/qwen2.5-coder-32b-instruct-q4_k_m.gguf"},
	{Name: "deepseek-coder-6.7b-q4_k_m.gguf", Size: "3.8 GB", URL: "https://huggingface.co/TheBloke/deepseek-coder-6.7B-instruct-GGUF/resolve/main/deepseek-coder-6.7b-instruct-q4_k_m.gguf"},
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
	if _, err := os.Stat(dest); err == nil {
		fmt.Println("Model already downloaded")
		return nil
	}

	fmt.Printf("Downloading %s...\n", modelName)
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	dst, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer dst.Close()

	_, err = io.Copy(dst, resp.Body)
	if err != nil {
		return fmt.Errorf("save failed: %w", err)
	}

	fmt.Printf("Downloaded to %s\n", dest)
	return nil
}