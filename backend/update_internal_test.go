package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.7.1", "1.7.0", true},
		{"v1.8.0", "1.7.9", true},
		{"1.7.0", "1.7.0", false},
		{"1.6.9", "1.7.0", false},
		{"1.7.0", "1.6.0-plus", true},
		{"2.0.0-beta", "1.9.9", true},
	}
	for _, c := range cases {
		if got := isNewer(c.a, c.b); got != c.want {
			t.Errorf("isNewer(%q,%q)=%v", c.a, c.b, got)
		}
	}
}

func TestUpdateFromRelease(t *testing.T) {
	digest := "sha256:" + hex.EncodeToString(make([]byte, 32))
	rel := ghRelease{
		TagName: "v1.9.0",
		HTMLURL: "https://github.com/x/y/releases/tag/v1.9.0",
		Assets: []ghAsset{
			{Name: "pwdtt-plus-linux-amd64", BrowserDownloadURL: "l"},
			{Name: "pwdtt-plus-windows-amd64.exe.sha256", BrowserDownloadURL: "s"},
			{Name: "pwdtt-plus-windows-amd64.exe", BrowserDownloadURL: "w", Size: 10, Digest: digest},
		},
	}
	info := updateFromRelease(rel, "1.7.0")
	if !info.Available || info.Version != "1.9.0" {
		t.Fatalf("%+v", info)
	}
	if runtime.GOOS == "windows" {
		if info.URL != "w" || !info.CanInstall || info.checksumURL != "s" || len(info.sha256) != 64 {
			t.Fatalf("windows asset: %+v", info)
		}
	}
	if updateFromRelease(rel, "1.9.0").Available {
		t.Fatal("same version must not be available")
	}
	rel.Prerelease = true
	if updateFromRelease(rel, "1.0.0").Available {
		t.Fatal("prerelease must be ignored")
	}
}

func TestDownloadAndChecksum(t *testing.T) {
	payload := append([]byte("MZ"), make([]byte, 100000)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "x.download")
	var last int
	sum, err := downloadFile(srv.URL, dst, int64(len(payload)), func(p int) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(payload)
	if sum != hex.EncodeToString(want[:]) || last != 100 {
		t.Fatalf("sum/progress mismatch: %s %d", sum, last)
	}
	if err := checkExecutable(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := downloadFile(srv.URL, dst, int64(len(payload))+1, func(int) {}); err == nil {
		t.Fatal("size mismatch must fail")
	}
	_ = os.WriteFile(dst, []byte("<html>"), 0o644)
	if checkExecutable(dst) == nil {
		t.Fatal("non-PE must fail")
	}
}
