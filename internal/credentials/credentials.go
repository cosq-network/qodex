// Package credentials stores provider secrets outside Qodex configuration.
package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// Save stores a provider token in the native credential store. On macOS this
// uses Keychain; the token is never written to a Qodex file.
func Save(projectRoot, tokenEnv, token string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("native credential storage is not implemented on %s", runtime.GOOS)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("cannot store an empty provider token")
	}
	service := serviceName(tokenEnv)
	account := currentAccount()
	cmd := exec.CommandContext(context.Background(), "security", "add-generic-password", "-a", account, "-s", service, "-w", token, "-U")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store provider credential in Keychain: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Load retrieves a provider token from the native credential store.
func Load(projectRoot, tokenEnv string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("native credential storage is not implemented on %s", runtime.GOOS)
	}
	services := []string{serviceName(tokenEnv), legacyServiceName(projectRoot, tokenEnv)}
	var lastErr error
	for _, service := range services {
		cmd := exec.CommandContext(context.Background(), "security", "find-generic-password", "-a", currentAccount(), "-s", service, "-w")
		output, err := cmd.Output()
		if err != nil {
			lastErr = err
			continue
		}
		token := strings.TrimSpace(string(output))
		if token != "" {
			return token, nil
		}
		lastErr = fmt.Errorf("empty provider credential")
	}
	return "", lastErr
}

func serviceName(tokenEnv string) string {
	sum := sha256.Sum256([]byte("global\x00" + tokenEnv))
	return "com.qodex.provider." + hex.EncodeToString(sum[:])
}

func legacyServiceName(projectRoot, tokenEnv string) string {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		root = projectRoot
	}
	sum := sha256.Sum256([]byte(root + "\x00" + tokenEnv))
	return "com.qodex.provider." + hex.EncodeToString(sum[:])
}

func currentAccount() string {
	if account, err := user.Current(); err == nil && account.Username != "" {
		return account.Username
	}
	return "qodex"
}
