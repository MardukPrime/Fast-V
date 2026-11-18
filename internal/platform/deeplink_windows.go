//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// userClassesRoot is the per-user registration root for URL protocol handlers.
// Writing there needs no administrator rights, and the current user's handler
// takes precedence over an HKLM registration of the same scheme.
const userClassesRoot = `Software\Classes`

// protocolDescription is the friendly name Windows shows for the scheme.
const protocolDescription = "URL:FastV Protocol"

// RegisterURLScheme registers scheme as a URL protocol owned by exePath in the
// current user's registry (HKCU\Software\Classes\<scheme>):
//
//	(Default)                  = "URL:FastV Protocol"
//	URL Protocol               = ""
//	shell\open\command\(Default) = "\"<exePath>\" \"%1\""
//
// Registering the same scheme again only overwrites the existing values, so the
// application can call it on every startup without tracking whether the handler
// is already installed.
func RegisterURLScheme(scheme string, exePath string) error {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		return errors.New("协议名称不能为空")
	}
	if !validRegistryScheme(scheme) {
		return fmt.Errorf("协议名称格式无效: %s", scheme)
	}
	exePath = strings.TrimSpace(exePath)
	if exePath == "" {
		return errors.New("程序路径不能为空")
	}
	if strings.ContainsAny(exePath, "\"\r\n") {
		return errors.New("程序路径包含无效字符")
	}

	keyPath := urlSchemeKeyPath(scheme)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.WRITE)
	if err != nil {
		return fmt.Errorf("创建协议注册表项失败: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue("", protocolDescription); err != nil {
		return fmt.Errorf("写入协议描述失败: %w", err)
	}
	if err := key.SetStringValue("URL Protocol", ""); err != nil {
		return fmt.Errorf("写入 URL Protocol 标记失败: %w", err)
	}

	commandKey, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath+`\shell\open\command`, registry.WRITE)
	if err != nil {
		return fmt.Errorf("创建协议启动命令失败: %w", err)
	}
	defer commandKey.Close()
	// Windows appends the opened link as the %1 argument, so the executable path
	// and the link are both quoted to survive paths containing spaces.
	command := urlSchemeCommand(exePath)
	if err := commandKey.SetStringValue("", command); err != nil {
		return fmt.Errorf("写入协议启动命令失败: %w", err)
	}
	return nil
}

// urlSchemeKeyPath returns the current-user registry path of one URL protocol
// handler.
func urlSchemeKeyPath(scheme string) string {
	return userClassesRoot + `\` + scheme
}

// urlSchemeCommand returns the shell open command of one URL protocol handler.
// Windows replaces %1 with the opened link.
func urlSchemeCommand(exePath string) string {
	return `"` + exePath + `" "%1"`
}

// validRegistryScheme reports whether scheme is a syntactically valid URL
// scheme (RFC 3986: ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )).
func validRegistryScheme(scheme string) bool {
	if scheme == "" {
		return false
	}
	for index := 0; index < len(scheme); index++ {
		character := scheme[index]
		switch {
		case character >= 'a' && character <= 'z':
		case index > 0 && character >= '0' && character <= '9':
		case index > 0 && (character == '+' || character == '-' || character == '.'):
		default:
			return false
		}
	}
	return true
}
