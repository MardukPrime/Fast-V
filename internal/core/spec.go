package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Kind identifies one supported proxy core.
type Kind string

const (
	KindXray    Kind = "xray"
	KindSingBox Kind = "singbox"
)

// coreSpec describes where one core comes from and how its binary behaves.
// The desktop app ships Xray-core today; sing-box is an alternative core the
// user can download from the settings page.
type coreSpec struct {
	Kind        Kind
	DisplayName string
	ReleaseAPI  string
	DirName     string
	BinaryName  string
	GeoData     bool
}

func specForKind(kind Kind) (coreSpec, error) {
	switch kind {
	case KindXray:
		return xraySpec(), nil
	case KindSingBox:
		return singBoxSpec(), nil
	default:
		return coreSpec{}, fmt.Errorf("unsupported core: %s", kind)
	}
}

func xraySpec() coreSpec {
	name := "xray"
	if runtime.GOOS == "windows" {
		name = "xray.exe"
	}
	return coreSpec{
		Kind:        KindXray,
		DisplayName: "Xray-core",
		ReleaseAPI:  "https://api.github.com/repos/XTLS/Xray-core/releases/latest",
		DirName:     "xray",
		BinaryName:  name,
		GeoData:     true,
	}
}

func singBoxSpec() coreSpec {
	name := "sing-box"
	if runtime.GOOS == "windows" {
		name = "sing-box.exe"
	}
	return coreSpec{
		Kind:        KindSingBox,
		DisplayName: "sing-box",
		ReleaseAPI:  "https://api.github.com/repos/SagerNet/sing-box/releases/latest",
		DirName:     "singbox",
		BinaryName:  name,
		GeoData:     false,
	}
}

// assetName returns the release asset for the current platform. Xray asset
// names do not contain the version; sing-box names do.
func (s coreSpec) assetName(tag string) (string, error) {
	if s.Kind == KindXray {
		return xrayAssetName()
	}
	version := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	suffix := ""
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			suffix = "windows-amd64.zip"
		case "arm64":
			suffix = "windows-arm64.zip"
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			suffix = "darwin-amd64.tar.gz"
		case "arm64":
			suffix = "darwin-arm64.tar.gz"
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			suffix = "linux-amd64.tar.gz"
		case "arm64":
			suffix = "linux-arm64.tar.gz"
		}
	}
	if suffix == "" {
		return "", fmt.Errorf("unsupported platform for sing-box: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("sing-box-%s-%s", version, suffix), nil
}

// versionArgs returns the CLI arguments that print the core version.
func (s coreSpec) versionArgs() []string { return []string{"version"} }

// readVersion parses `<binary> version` output.
func (s coreSpec) readVersion(ctx context.Context, binary string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, binary, s.versionArgs()...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to read %s version: %w", s.DisplayName, err)
	}
	line := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	fields := strings.Fields(line)
	if s.Kind == KindSingBox {
		for i, field := range fields {
			if strings.EqualFold(field, "version") && i+1 < len(fields) {
				return strings.TrimPrefix(fields[i+1], "v"), nil
			}
		}
	}
	if len(fields) >= 2 && (strings.EqualFold(fields[0], "Xray") || strings.EqualFold(fields[0], s.DisplayName)) {
		return strings.TrimPrefix(fields[1], "v"), nil
	}
	return "", fmt.Errorf("unexpected %s version output: %s", s.DisplayName, line)
}

// runArgs returns the arguments that run the generated configuration.
func (s coreSpec) runArgs(configPath string) []string {
	if s.Kind == KindSingBox {
		return []string{"run", "-c", configPath}
	}
	return []string{"run", "-config", configPath}
}

// validateArgs returns the arguments that validate a configuration without
// starting the core.
func (s coreSpec) validateArgs(configPath string) []string {
	if s.Kind == KindSingBox {
		return []string{"check", "-c", configPath}
	}
	return []string{"run", "-test", "-config", configPath}
}

// configFileName is the generated config file name inside the core directory.
func (s coreSpec) configFileName() string {
	if s.Kind == KindSingBox {
		return "sing-box-config.json"
	}
	return "xray-config.json"
}

// extractedBinary locates the core binary inside an extracted release archive.
func (s coreSpec) extractedBinary(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(entry.Name(), s.BinaryName) {
			found = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("downloaded archive does not contain " + s.BinaryName)
	}
	return found, nil
}

// activeCoreInfo is a small helper shared by the core service.
func activeCoreKind(coreType string) Kind {
	if strings.EqualFold(strings.TrimSpace(coreType), string(KindSingBox)) {
		return KindSingBox
	}
	return KindXray
}
