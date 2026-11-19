package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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

func installCoreFiles(sourceDir string, destinationDir string, spec coreSpec) error {
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		isBinary := strings.EqualFold(entry.Name(), spec.BinaryName)
		target := filepath.Join(destinationDir, filepath.Base(relative))
		mode := os.FileMode(0o600)
		if isBinary {
			mode = 0o755
			target = filepath.Join(destinationDir, spec.BinaryName)
		}
		return copyFileAtomic(path, target, mode)
	})
}

// extractArchive extracts a .zip or .tar.gz/.tgz core release archive.
func extractArchive(archivePath string, destination string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destination)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destination)
	default:
		// Staging files have no extension; sniff the ZIP magic instead.
		head := make([]byte, 4)
		file, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		_, readErr := io.ReadFull(file, head)
		_ = file.Close()
		if readErr == nil && head[0] == 'P' && head[1] == 'K' {
			return extractZip(archivePath, destination)
		}
		return fmt.Errorf("unsupported core archive format")
	}
}

func extractTarGz(archivePath string, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	cleanDestination := filepath.Clean(destination) + string(os.PathSeparator)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(header.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(destination, name)
		if !strings.HasPrefix(filepath.Clean(target), cleanDestination) {
			return fmt.Errorf("invalid archive path: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			dest, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(dest, reader); err != nil {
				dest.Close()
				return err
			}
			if err := dest.Close(); err != nil {
				return err
			}
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
