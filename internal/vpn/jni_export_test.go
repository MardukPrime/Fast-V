package vpn

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestAndroidJNINativeSymbolsMatch guards the one seam that cannot be compiled on
// a host machine: Java declares `native` methods that Go must export under the
// exact JNI symbol name. A typo there compiles on both sides and only fails on a
// device with UnsatisfiedLinkError, so the two files are compared here.
func TestAndroidJNINativeSymbolsMatch(t *testing.T) {
	root := repoRoot(t)
	bridgePath := filepath.Join(root, "packaging", "android", "app", "src", "main", "java", "com", "wails", "app", "WailsBridge.java")
	jniPath := filepath.Join(root, "android_vpn_jni.go")

	bridge, err := os.ReadFile(bridgePath)
	if err != nil {
		t.Fatalf("read WailsBridge.java: %v", err)
	}
	jni, err := os.ReadFile(jniPath)
	if err != nil {
		t.Fatalf("read android_vpn_jni.go: %v", err)
	}

	declared := regexp.MustCompile(`static\s+native\s+[\w\[\]]+\s+(nativeVpn\w+)\s*\(`).FindAllStringSubmatch(string(bridge), -1)
	if len(declared) == 0 {
		t.Fatalf("no nativeVpn* declarations found in WailsBridge.java; the VPN bridge was renamed or removed")
	}

	exported := make(map[string]bool)
	for _, match := range regexp.MustCompile(`//export\s+Java_com_wails_app_WailsBridge_(\w+)`).FindAllStringSubmatch(string(jni), -1) {
		exported[match[1]] = true
	}

	for _, match := range declared {
		name := match[1]
		if !exported[name] {
			t.Errorf("%s is declared in WailsBridge.java but Java_com_wails_app_WailsBridge_%s is not exported from android_vpn_jni.go", name, name)
		}
	}

	// The reverse direction catches exports that Java never calls, which is dead
	// code at best and a stale rename at worst.
	for name := range exported {
		found := false
		for _, match := range declared {
			if match[1] == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("android_vpn_jni.go exports %s but WailsBridge.java declares no such native", name)
		}
	}
}

// TestAndroidServiceIsDeclared keeps the VpnService reachable from the manifest:
// without the service entry the app can never open a tun interface.
func TestAndroidServiceIsDeclared(t *testing.T) {
	root := repoRoot(t)
	manifest, err := os.ReadFile(filepath.Join(root, "packaging", "android", "app", "src", "main", "AndroidManifest.xml"))
	if err != nil {
		t.Fatalf("read AndroidManifest.xml: %v", err)
	}
	text := string(manifest)
	for _, want := range []string{
		androidServiceName,
		`android:foregroundServiceType="specialUse"`,
		`android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE"`,
		`android:name="android.permission.BIND_VPN_SERVICE"`,
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(text) {
			t.Errorf("AndroidManifest.xml is missing %q", want)
		}
	}
}

const androidServiceName = `android:name=".FastVVpnService"`

// repoRoot walks up from the test file until the module root is found.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the repository root")
		}
		dir = parent
	}
}
