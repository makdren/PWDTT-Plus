package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// UpdateRepo — репозиторий, из релизов которого приходят обновления.
const UpdateRepo = "makdren/PWDTT-Plus"

// UpdateInfo — информация о доступном обновлении.
type UpdateInfo struct {
	Available   bool   `json:"available"`   // есть ли обновление
	Version     string `json:"version"`     // версия обновления
	URL         string `json:"url"`         // ссылка на файл (exe) для скачивания
	Body        string `json:"body"`        // описание изменений (changelog)
	PageURL     string `json:"pageUrl"`     // страница релиза
	CanInstall  bool   `json:"canInstall"`  // можно обновиться из приложения
	Size        int64  `json:"size"`        // размер файла
	sha256      string // ожидаемый хеш (из digest GitHub или файла .sha256)
	checksumURL string
}

type ghAsset struct {
	BrowserDownloadURL string `json:"browser_download_url"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"` // "sha256:<hex>" (GitHub добавляет сам)
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

var updateHTTP = &http.Client{Timeout: 15 * time.Second}

func githubGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PWDTT-Plus/"+Version)
	return updateHTTP.Do(req)
}

// CheckUpdate проверяет последний релиз на GitHub (UpdateRepo).
// Возвращает ошибку, если сеть или API недоступны.
func CheckUpdate(currentVersion string) (*UpdateInfo, error) {
	resp, err := githubGet("https://api.github.com/repos/" + UpdateRepo + "/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return &UpdateInfo{Available: false}, nil // релизов ещё нет
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var release ghRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return updateFromRelease(release, currentVersion), nil
}

func updateFromRelease(release ghRelease, currentVersion string) *UpdateInfo {
	latest := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	newer := isNewer(latest, currentVersion)
	log.Printf("[UPDATE] Release: %s, Current: %s, isNewer: %v", latest, currentVersion, newer)
	if release.Draft || release.Prerelease || !newer {
		return &UpdateInfo{Available: false}
	}
	info := &UpdateInfo{
		Available: true,
		Version:   latest,
		Body:      release.Body,
		PageURL:   release.HTMLURL,
	}
	if info.PageURL == "" {
		info.PageURL = "https://github.com/" + UpdateRepo + "/releases/latest"
	}
	asset := pickAsset(release.Assets)
	if asset == nil {
		return info
	}
	info.URL = asset.BrowserDownloadURL
	info.Size = asset.Size
	if d, ok := strings.CutPrefix(strings.ToLower(asset.Digest), "sha256:"); ok && len(d) == 64 {
		info.sha256 = d
	}
	for _, a := range release.Assets {
		if strings.EqualFold(a.Name, asset.Name+".sha256") {
			info.checksumURL = a.BrowserDownloadURL
		}
	}
	info.CanInstall = runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(asset.Name), ".exe")
	return info
}

// pickAsset выбирает файл под текущую ОС: для Windows — .exe с "windows" в
// имени (например pwdtt-plus-windows-amd64.exe), иначе любой .exe.
func pickAsset(assets []ghAsset) *ghAsset {
	osName := runtime.GOOS
	osAliases := []string{osName}
	if osName == "darwin" {
		osAliases = append(osAliases, "macos", "mac", "osx")
	}
	var fallback *ghAsset
	for i := range assets {
		a := &assets[i]
		name := strings.ToLower(a.Name)
		if strings.HasSuffix(name, ".sha256") || strings.HasSuffix(name, ".txt") {
			continue
		}
		if osName == "windows" && !strings.HasSuffix(name, ".exe") {
			continue
		}
		if strings.Contains(name, "arm64") && runtime.GOARCH != "arm64" {
			continue
		}
		for _, alias := range osAliases {
			if strings.Contains(name, alias) {
				return a
			}
		}
		if fallback == nil && osName == "windows" {
			fallback = a
		}
	}
	return fallback
}

// isNewer проверяет что a > b (semver сравнение).
func isNewer(a, b string) bool {
	av := parseVersion(a)
	bv := parseVersion(b)
	for i := 0; i < 3; i++ {
		if av[i] > bv[i] {
			return true
		}
		if av[i] < bv[i] {
			return false
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	var result [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	for i, p := range parts {
		fmt.Sscanf(p, "%d", &result[i])
	}
	return result
}
