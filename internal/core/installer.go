package core

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func downloadFile(ctx context.Context, client *http.Client, url string, destination string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FastV/0.1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download Xray-core: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Xray-core download returned HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(destination)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hash), resp.Body); err != nil {
		return "", fmt.Errorf("failed to save Xray-core download: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestMatches(digest string, actual string) bool {
	digest = strings.TrimSpace(digest)
	if strings.HasPrefix(digest, "sha256:") {
		return strings.EqualFold(strings.TrimPrefix(digest, "sha256:"), actual)
	}
	return false
}

func extractZip(archivePath string, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}

	for _, file := range reader.File {
		name := filepath.Clean(file.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(destination, name)
		cleanDestination := filepath.Clean(destination) + string(os.PathSeparator)
		if !strings.HasPrefix(filepath.Clean(target), cleanDestination) {
			return fmt.Errorf("invalid archive path: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o600
		}
		dest, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(dest, source)
		source.Close()
		dest.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func installCoreFiles(sourceDir string, destinationDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		source := filepath.Join(sourceDir, entry.Name())
		target := filepath.Join(destinationDir, entry.Name())
		mode := os.FileMode(0o600)
		if entry.Name() == coreBinaryName() {
			mode = 0o755
		}
		if err := copyFileAtomic(source, target, mode); err != nil {
			return err
		}
	}
	return nil
}

func copyFileAtomic(source string, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	temp := destination + ".new"
	output, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, destination); err != nil {
		_ = os.Remove(destination)
		if err := os.Rename(temp, destination); err != nil {
			_ = os.Remove(temp)
			return err
		}
	}
	return nil
}

func writeCoreVersionRecord(directory string, record coreVersionRecord) error {
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "version.json"), raw, 0o600)
}
