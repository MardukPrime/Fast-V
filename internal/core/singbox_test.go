package core

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSingBoxAssetName(t *testing.T) {
	spec := singBoxSpec()
	name, err := spec.assetName("v1.14.4")
	if err != nil {
		t.Fatalf("assetName: %v", err)
	}
	var want string
	switch runtime.GOOS {
	case "windows":
		want = "sing-box-1.14.4-windows-amd64.zip"
	case "darwin":
		want = "sing-box-1.14.4-darwin-amd64.tar.gz"
	case "linux":
		want = "sing-box-1.14.4-linux-amd64.tar.gz"
	}
	if name != want {
		t.Fatalf("assetName = %q, want %q", name, want)
	}
}

func TestSingBoxExtractedBinaryFindsNestedFile(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "sing-box-1.14.4-windows-amd64")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(nested, singBoxSpec().BinaryName)
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	found, err := singBoxSpec().extractedBinary(root)
	if err != nil {
		t.Fatalf("extractedBinary: %v", err)
	}
	if found != binary {
		t.Fatalf("found = %q, want %q", found, binary)
	}
}

func TestInstallCoreFilesKeepsNestedRuntimeFiles(t *testing.T) {
	source := t.TempDir()
	nested := filepath.Join(source, "sing-box-1.14.4-windows-amd64")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		singBoxSpec().BinaryName: "binary",
		"libcronet.dll":          "dll",
	} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	destination := t.TempDir()
	if err := installCoreFiles(source, destination, singBoxSpec()); err != nil {
		t.Fatalf("installCoreFiles: %v", err)
	}
	for _, name := range []string{singBoxSpec().BinaryName, "libcronet.dll"} {
		if _, err := os.Stat(filepath.Join(destination, name)); err != nil {
			t.Fatalf("%s not installed: %v", name, err)
		}
	}
}

func TestExtractTarGz(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "sing-box.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("nested binary")
	header := &tar.Header{
		Name: "sing-box-1.14.4-linux-amd64/sing-box",
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "extracted")
	if err := extractArchive(archivePath, destination); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "sing-box-1.14.4-linux-amd64", "sing-box"))
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if !strings.Contains(string(data), "nested binary") {
		t.Fatalf("unexpected content %q", string(data))
	}
}
