package core

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type coreVersionRecord struct {
	Version     string `json:"version"`
	AssetName   string `json:"assetName"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installedAt"`
}

func coreBinaryName() string {
	if runtime.GOOS == "windows" {
		return "xray.exe"
	}
	return "xray"
}

func findReleaseAsset(release *releaseInfo) (releaseAsset, error) {
	wanted, err := coreAssetName()
	if err != nil {
		return releaseAsset{}, err
	}
	for _, asset := range release.Assets {
		if asset.Name == wanted {
			return asset, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("Xray-core release has no asset %s", wanted)
}

func coreAssetName() (string, error) {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-windows-64.zip", nil
		case "arm64":
			return "Xray-windows-arm64-v8a.zip", nil
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-linux-64.zip", nil
		case "arm64":
			return "Xray-linux-arm64-v8a.zip", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-macos-64.zip", nil
		case "arm64":
			return "Xray-macos-arm64-v8a.zip", nil
		}
	}
	return "", fmt.Errorf("unsupported platform for Xray-core: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func readCoreVersion(ctx context.Context, binary string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	output, err := exec.CommandContext(commandCtx, binary, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to read Xray-core version: %w", err)
	}
	line := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	fields := strings.Fields(line)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "Xray") {
		return strings.TrimPrefix(fields[1], "v"), nil
	}
	return "", fmt.Errorf("unexpected Xray-core version output: %s", line)
}

func compareVersions(a, b string) int {
	left := parseVersionParts(a)
	right := parseVersionParts(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l > r {
			return 1
		}
		if l < r {
			return -1
		}
	}
	return 0
}

func parseVersionParts(value string) []int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		number := 0
		for _, char := range part {
			if char < '0' || char > '9' {
				break
			}
			number = number*10 + int(char-'0')
		}
		out = append(out, number)
	}
	return out
}
