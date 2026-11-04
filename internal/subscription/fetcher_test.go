package subscription

import "testing"

func TestParseUserInfo(t *testing.T) {
	info := ParseUserInfo("upload=1073741824; download=2147483648; total=10737418240; expire=1893456000")
	if !info.Present {
		t.Fatal("expected user info to be present")
	}
	if info.UploadBytes != 1073741824 || info.DownloadBytes != 2147483648 || info.TotalBytes != 10737418240 {
		t.Fatalf("unexpected traffic info: %+v", info)
	}
	if info.ExpireAt != 1893456000 {
		t.Fatalf("unexpected expiry: %+v", info)
	}
}

func TestParseUserInfoExpireMilliseconds(t *testing.T) {
	info := ParseUserInfo("expire=1893456000000")
	if info.ExpireAt != 1893456000 {
		t.Fatalf("expected milliseconds to be normalized: %+v", info)
	}
}

func TestParseUserInfoMissing(t *testing.T) {
	info := ParseUserInfo("")
	if info.Present {
		t.Fatalf("empty header should not be present: %+v", info)
	}
}
