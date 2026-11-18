// Package update implements the FastV application self-update check against
// the GitHub release feed.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
)

// DefaultAPIURL is the GitHub "latest release" endpoint of the FastV
// repository.
const DefaultAPIURL = "https://api.github.com/repos/MardukPrime/Fast-V/releases/latest"

const (
	// defaultTimeout bounds one complete release request when the caller's
	// context has no shorter deadline and no explicit client is configured.
	defaultTimeout = 20 * time.Second
	// maxReleaseBody bounds how much of the response is read before decoding.
	maxReleaseBody = 1 << 20
)

// Checker fetches the latest published FastV release and compares it with the
// running application version.
type Checker struct {
	// APIURL overrides the release endpoint. An empty value means
	// DefaultAPIURL.
	APIURL string
	// Client is the HTTP client used for the request. A nil value means a
	// client with defaultTimeout.
	Client *http.Client
}

// New returns a Checker configured for the FastV GitHub repository.
func New() *Checker {
	return &Checker{APIURL: DefaultAPIURL}
}

// release mirrors the GitHub release payload fields FastV consumes.
type release struct {
	TagName string         `json:"tag_name"`
	Body    string         `json:"body"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

// releaseAsset mirrors one GitHub release asset entry.
type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Asset selection ranks. Higher is better; zero means the asset is not a
// usable update package for the current platform.
const (
	// assetRankFallback is a platform-specific fallback package (an .exe on
	// Windows) that does not match the release archive convention.
	assetRankFallback = 1
	// assetRankPlatform is an archive for the current platform without an
	// explicit architecture marker (for example a "universal" macOS build).
	assetRankPlatform = 2
	// assetRankExact is the preferred package: archive for the current
	// platform and CPU architecture.
	assetRankExact = 3
)

// Legacy Windows-specific rank aliases, kept so older call sites and tests
// keep their meaning while asset ranking became platform-aware.
const (
	assetRankFastVExe        = assetRankFallback
	assetRankWindowsZip      = assetRankPlatform
	assetRankWindowsZipAMD64 = assetRankExact
)

// Check fetches the latest published release and compares it with
// currentVersion. A failed check (network error, non-200 response, malformed
// payload) is reported both in UpdateInfo.Error and as the returned error.
// Check never panics, so callers can always display the UpdateInfo they get.
func (c *Checker) Check(ctx context.Context, currentVersion string) (model.UpdateInfo, error) {
	info := model.UpdateInfo{
		CurrentVersion: normalizeVersion(currentVersion),
		CheckedAt:      model.NowRFC3339(),
	}

	latest, err := c.fetch(ctx)
	if err != nil {
		info.Error = err.Error()
		return info, err
	}

	info.LatestVersion = normalizeVersion(latest.TagName)
	info.Notes = strings.TrimSpace(latest.Body)
	info.URL = strings.TrimSpace(latest.HTMLURL)
	info.Available = CompareVersions(info.LatestVersion, info.CurrentVersion) > 0

	// A missing platform asset is not an error: the release may only publish
	// source archives, in which case AssetURL stays empty and the UI keeps
	// showing the release page instead of a download button.
	if asset, ok := selectReleaseAsset(latest.Assets); ok {
		info.AssetName = strings.TrimSpace(asset.Name)
		info.AssetURL = strings.TrimSpace(asset.BrowserDownloadURL)
		info.AssetSize = asset.Size
	}
	return info, nil
}

// selectReleaseAsset picks the best update package for the current runtime
// (GOOS/GOARCH) from the release assets. The second result is false when none
// of the assets is usable on this platform.
func selectReleaseAsset(assets []releaseAsset) (releaseAsset, bool) {
	selected := releaseAsset{}
	selectedRank := 0
	for _, asset := range assets {
		rank := assetRankFor(runtime.GOOS, runtime.GOARCH, asset.Name)
		if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			rank = 0
		}
		if rank > selectedRank {
			selected = asset
			selectedRank = rank
		}
	}
	return selected, selectedRank > 0
}

// assetRank scores one release asset for the current platform. Kept as a thin
// wrapper so existing call sites and tests stay valid.
func assetRank(asset releaseAsset) int {
	if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
		return 0
	}
	return assetRankFor(runtime.GOOS, runtime.GOARCH, asset.Name)
}

// assetRankFor scores one release asset for an explicit GOOS/GOARCH pair; zero
// means "not an update package for that platform". Names are matched
// case-insensitively and tolerate the usual architecture markers.
func assetRankFor(goos, goarch, assetName string) int {
	name := strings.ToLower(strings.TrimSpace(assetName))
	if name == "" {
		return 0
	}

	amd64Markers := []string{"amd64", "x64", "x86_64", "x86-64", "intel"}
	arm64Markers := []string{"arm64", "aarch64"}
	otherArchMarkers := []string{"386", "armv7", "armv6", "ppc64le", "ppc64", "riscv64", "s390x"}

	// A marker for a different architecture means the asset is not for this
	// build, even if the platform matches.
	switch goarch {
	case "amd64":
		if containsAny(name, arm64Markers) || containsAny(name, otherArchMarkers) {
			return 0
		}
	case "arm64":
		if containsAny(name, amd64Markers) || containsAny(name, otherArchMarkers) {
			return 0
		}
	default:
		if containsAny(name, amd64Markers) || containsAny(name, arm64Markers) {
			return 0
		}
	}

	archRank := func() int {
		if strings.Contains(name, "universal") {
			return assetRankPlatform
		}
		ownMarkers := amd64Markers
		if goarch == "arm64" {
			ownMarkers = arm64Markers
		}
		if containsAny(name, ownMarkers) {
			return assetRankExact
		}
		return assetRankPlatform
	}

	switch goos {
	case "windows":
		if strings.Contains(name, "windows") && strings.HasSuffix(name, ".zip") {
			return archRank()
		}
		if strings.HasSuffix(name, ".exe") && isFastVExecutableName(name) {
			return assetRankFallback
		}
		return 0
	case "darwin":
		if !strings.Contains(name, "macos") && !strings.Contains(name, "darwin") && !strings.Contains(name, "mac") {
			return 0
		}
		if !strings.HasSuffix(name, ".zip") {
			return 0
		}
		return archRank()
	case "linux":
		if !strings.Contains(name, "linux") {
			return 0
		}
		if !strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".tgz") && !strings.HasSuffix(name, ".zip") {
			return 0
		}
		return archRank()
	default:
		return 0
	}
}

// containsAny reports whether name contains at least one marker.
func containsAny(name string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// isAMD64AssetName reports whether a release asset name names the amd64/x64
// architecture.
func isAMD64AssetName(name string) bool {
	for _, marker := range []string{"amd64", "x64", "x86_64", "x86-64"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// isFastVExecutableName reports whether name is a FastV executable. Both
// "FastV.exe" and "Fast-V-setup.exe" style names are accepted.
func isFastVExecutableName(name string) bool {
	if !strings.HasSuffix(name, ".exe") {
		return false
	}
	compact := strings.NewReplacer("-", "", "_", "", " ", "").Replace(name)
	return strings.Contains(compact, "fastv")
}

// fetch requests and decodes the latest release document.
func (c *Checker) fetch(ctx context.Context) (release, error) {
	endpoint := strings.TrimSpace(c.APIURL)
	if endpoint == "" {
		endpoint = DefaultAPIURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, fmt.Errorf("check update: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FastV/"+normalizeVersion(buildinfo.Version))

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return release{}, fmt.Errorf("check update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("check update: GitHub returned HTTP %d", resp.StatusCode)
	}

	var latest release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseBody)).Decode(&latest); err != nil {
		return release{}, fmt.Errorf("check update: decode release: %w", err)
	}
	if strings.TrimSpace(latest.TagName) == "" {
		return release{}, errors.New("check update: release is missing tag_name")
	}
	return latest, nil
}

func (c *Checker) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: defaultTimeout}
}

// CompareVersions compares two version strings and returns -1, 0 or 1.
//
// A leading "v" is optional and prerelease/build suffixes are ignored, so
// "v1.2.3-beta.1" equals "1.2.3". Missing numeric segments count as zero, so
// "1.2" equals "1.2.0". Unparsable input is treated as 0.0.0 instead of
// failing, which keeps a local development build usable for update checks.
func CompareVersions(a, b string) int {
	left := parseVersion(a)
	right := parseVersion(b)
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

// parseVersion splits a version string into its numeric segments. Everything
// from the first "-" (prerelease) or "+" (build metadata) on is dropped.
func parseVersion(value string) []int {
	value = normalizeVersion(value)
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}

	parts := strings.Split(value, ".")
	segments := make([]int, 0, len(parts))
	for _, part := range parts {
		segments = append(segments, parseVersionSegment(part))
	}
	return segments
}

// parseVersionSegment converts the leading digits of part to an int. A segment
// without digits (for example "beta") is treated as zero.
func parseVersionSegment(part string) int {
	part = strings.TrimSpace(part)
	digits := 0
	for digits < len(part) && part[digits] >= '0' && part[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return 0
	}
	number, err := strconv.Atoi(part[:digits])
	if err != nil {
		return 0
	}
	return number
}

// normalizeVersion trims surrounding whitespace and a single optional "v"/"V"
// prefix.
func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "V")
	return value
}
