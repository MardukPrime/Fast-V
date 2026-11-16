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
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

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
	return info, nil
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
