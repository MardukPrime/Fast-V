// Package deeplink parses the fastv:// deep links that FastV registers with the
// operating system, and the raw share links that may be passed on the command
// line. Parsing is pure: it never touches the file system or the network.
package deeplink

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// Scheme is the custom URL scheme registered for FastV deep links.
const Scheme = "fastv"

// Request kinds returned by Parse.
const (
	// KindSubscription asks FastV to add the subscription found at URL.
	KindSubscription = "subscription"
	// KindNode asks FastV to import the share link found at URL as a manual node.
	KindNode = "node"
)

// Actions (the part after "fastv://") understood by the parser.
const (
	actionImport = "import"
	actionNode   = "node"
)

// maxErrorValueLength bounds how much of an unrecognized link is echoed in an
// error message, so a malformed multi-kilobyte share link cannot flood the log.
const maxErrorValueLength = 80

// Request is one parsed deep link.
type Request struct {
	// Kind is either KindSubscription or KindNode.
	Kind string
	// URL is the subscription URL or the share link to import.
	URL string
}

// supportedShareSchemes lists the node share link schemes FastV can import.
var supportedShareSchemes = []string{
	"vless",
	"vmess",
	"trojan",
	"ss",
	"hysteria2",
	"tuic",
}

// Parse validates one deep link argument and returns the import request it
// describes.
//
// Accepted forms:
//   - fastv://import?url=<subscription URL>
//   - fastv://node?link=<share link> (url= is accepted as an alias)
//   - a raw vless/vmess/trojan/ss/hysteria2/tuic share link
//
// The argument may be surrounded by whitespace and double quotes: the Windows
// protocol handler receives the %1 placeholder as part of a command line, and a
// shell or a hand written shortcut may hand the wrapping quotes to the process.
func Parse(raw string) (Request, error) {
	value := trimArgument(raw)
	if value == "" {
		return Request{}, errors.New("深链接为空")
	}
	if hasScheme(value, Scheme) {
		return parseFastVLink(value)
	}
	if request, ok := parseShareLink(value); ok {
		return request, nil
	}
	return Request{}, fmt.Errorf("无法识别的深链接: %s", displayValue(value))
}

// parseFastVLink handles the fastv://action?parameter links.
func parseFastVLink(value string) (Request, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return Request{}, fmt.Errorf("深链接格式无效: %v", err)
	}

	query := parsed.Query()
	switch action := fastvAction(parsed); action {
	case actionImport:
		target := trimArgument(firstQueryValue(query, "url"))
		if target == "" {
			return Request{}, errors.New("深链接缺少 url 参数")
		}
		// The import endpoint also accepts a bare share link as its url value.
		if request, ok := parseShareLink(target); ok {
			return request, nil
		}
		if err := validateSubscriptionURL(target); err != nil {
			return Request{}, err
		}
		return Request{Kind: KindSubscription, URL: target}, nil

	case actionNode:
		target := trimArgument(firstQueryValue(query, "link"))
		if target == "" {
			target = trimArgument(firstQueryValue(query, "url"))
		}
		if target == "" {
			return Request{}, errors.New("深链接缺少 link 参数")
		}
		_, link, ok := normalizedShareLink(target)
		if !ok {
			return Request{}, shareLinkError(target)
		}
		return Request{Kind: KindNode, URL: link}, nil

	case "":
		return Request{}, errors.New("深链接缺少类型（import 或 node）")
	default:
		return Request{}, fmt.Errorf("不支持的深链接类型: %s", action)
	}
}

// fastvAction returns the lower case action of a fastv link. Both the authority
// form (fastv://import) and the opaque/path forms some callers produce
// (fastv:import, fastv:///import) are supported.
func fastvAction(parsed *url.URL) string {
	for _, candidate := range []string{parsed.Host, parsed.Opaque, parsed.Path} {
		if action := strings.ToLower(strings.Trim(strings.TrimSpace(candidate), "/")); action != "" {
			return action
		}
	}
	return ""
}

// parseShareLink accepts a bare share link such as vless://...
func parseShareLink(value string) (Request, bool) {
	_, link, ok := normalizedShareLink(value)
	if !ok {
		return Request{}, false
	}
	return Request{Kind: KindNode, URL: link}, true
}

// normalizedShareLink validates a share link and returns its lower case scheme
// together with the normalized link. ok is false for unsupported or malformed
// links.
func normalizedShareLink(value string) (scheme string, link string, ok bool) {
	rawScheme, rest, found := strings.Cut(value, "://")
	if !found {
		return "", "", false
	}
	scheme = strings.ToLower(strings.TrimSpace(rawScheme))
	if !isSupportedShareScheme(scheme) {
		return "", "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", false
	}
	return scheme, scheme + "://" + rest, true
}

// validateSubscriptionURL enforces the http/https contract for subscriptions.
func validateSubscriptionURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return errors.New("订阅链接无效")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	default:
		return errors.New("订阅链接必须使用 http 或 https")
	}
}

// shareLinkError builds a specific error for an unsupported share link.
func shareLinkError(value string) error {
	rawScheme, _, found := strings.Cut(value, "://")
	if !found {
		return fmt.Errorf("分享链接格式无效: %s", displayValue(value))
	}
	scheme := strings.ToLower(strings.TrimSpace(rawScheme))
	if !isSupportedShareScheme(scheme) {
		return fmt.Errorf("不支持的分享链接协议: %s", scheme)
	}
	return fmt.Errorf("分享链接格式无效: %s", displayValue(value))
}

// firstQueryValue returns the first non-empty value stored under any of names.
func firstQueryValue(values url.Values, names ...string) string {
	for _, name := range names {
		for _, value := range values[name] {
			if strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return ""
}

// isSupportedShareScheme reports whether scheme is a supported node share link.
func isSupportedShareScheme(scheme string) bool {
	return slices.Contains(supportedShareSchemes, scheme)
}

// hasScheme reports whether value starts with the given URL scheme, ignoring
// case.
func hasScheme(value, scheme string) bool {
	return strings.HasPrefix(strings.ToLower(value), scheme+":")
}

// trimArgument removes surrounding whitespace and the wrapping quotes a shell
// may hand to the process, for example `"fastv://import?url=..."`.
func trimArgument(raw string) string {
	value := strings.TrimSpace(raw)
	for len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first != '"' && first != '\'') || first != last {
			break
		}
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

// displayValue shortens a value for an error message without splitting a rune.
func displayValue(value string) string {
	if utf8.RuneCountInString(value) <= maxErrorValueLength {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxErrorValueLength]) + "..."
}
