package backend

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Обновление из приложения: скачиваем exe из релиза GitHub рядом с текущим,
// проверяем SHA-256, подменяем файл и перезапускаемся (selfupdate_windows.go).

const maxUpdateSize = 200 << 20

var (
	updateMu      sync.Mutex
	lastUpdate    *UpdateInfo
	updateRunning bool
)

func rememberUpdate(info *UpdateInfo) {
	updateMu.Lock()
	lastUpdate = info
	updateMu.Unlock()
}

// InstallUpdate скачивает и устанавливает найденное обновление, затем
// перезапускает приложение. Прогресс — событие "update_progress" (0..100).
func (a *App) InstallUpdate() error {
	updateMu.Lock()
	if updateRunning {
		updateMu.Unlock()
		return errors.New("обновление уже выполняется")
	}
	info := lastUpdate
	updateRunning = true
	updateMu.Unlock()
	defer func() {
		updateMu.Lock()
		updateRunning = false
		updateMu.Unlock()
	}()

	if info == nil || !info.Available {
		fresh, err := CheckUpdate(Version)
		if err != nil {
			return fmt.Errorf("не удалось проверить обновление: %w", err)
		}
		info = fresh
		rememberUpdate(info)
	}
	if !info.Available {
		return errors.New("обновлений нет")
	}
	if !info.CanInstall || info.URL == "" {
		return errors.New("в релизе нет файла для Windows — скачайте обновление со страницы релиза")
	}

	exe, err := exePath()
	if err != nil {
		return fmt.Errorf("путь к программе: %w", err)
	}
	tmp := exe + ".download"
	_ = os.Remove(tmp)

	expected := info.sha256
	if expected == "" && info.checksumURL != "" {
		expected, _ = fetchChecksum(info.checksumURL)
	}

	sum, err := downloadFile(info.URL, tmp, info.Size, func(p int) {
		if a.ctx != nil {
			wails.EventsEmit(a.ctx, "update_progress", p)
		}
	})
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if expected != "" && !strings.EqualFold(sum, expected) {
		_ = os.Remove(tmp)
		return errors.New("контрольная сумма не совпала — файл повреждён, обновление отменено")
	}
	if err := checkExecutable(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if a.bridge != nil {
		a.bridge.Disconnect()
	}
	if err := replaceAndRestart(exe, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Новая версия запущена и ждёт завершения этого процесса.
	go func() {
		time.Sleep(300 * time.Millisecond)
		if a.ctx != nil {
			wails.Quit(a.ctx)
		} else {
			os.Exit(0)
		}
	}()
	return nil
}

func downloadFile(url, dst string, expectedSize int64, progress func(int)) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "PWDTT-Plus/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("скачивание: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("скачивание: HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = expectedSize
	}
	if total > maxUpdateSize {
		return "", errors.New("файл обновления слишком большой")
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", fmt.Errorf("нет доступа к папке программы: %w", err)
	}
	h := sha256.New()
	var written int64
	lastPct := -1
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			written += int64(n)
			if written > maxUpdateSize {
				f.Close()
				return "", errors.New("файл обновления слишком большой")
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return "", fmt.Errorf("запись файла: %w", werr)
			}
			h.Write(buf[:n])
			if total > 0 {
				if pct := int(written * 100 / total); pct != lastPct {
					lastPct = pct
					progress(pct)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return "", fmt.Errorf("скачивание прервано: %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if expectedSize > 0 && written != expectedSize {
		return "", errors.New("файл скачан не полностью")
	}
	progress(100)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchChecksum читает файл "<asset>.sha256" (формат sha256sum: "<hex>  <имя>").
func fetchChecksum(url string) (string, error) {
	resp, err := githubGet(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4096))
	if sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) > 0 && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", errors.New("неверный файл контрольной суммы")
}

// checkExecutable — скачанный файл должен быть исполняемым Windows (PE).
func checkExecutable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil || head[0] != 'M' || head[1] != 'Z' {
		return errors.New("скачанный файл не является программой Windows")
	}
	return nil
}

// CleanupAfterUpdate удаляет копию старой версии, оставшуюся после обновления.
func CleanupAfterUpdate() {
	exe, err := exePath()
	if err != nil {
		return
	}
	for i := 0; i < 10; i++ {
		if err := os.Remove(exe + ".old"); err == nil || os.IsNotExist(err) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = os.Remove(exe + ".download")
}
