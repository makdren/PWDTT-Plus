// Поддельный клиент WDTT Plus для тестов wdtt_engine.go: тот же протокол
// stdin/stdout, что у go_client. Пароль "bound" привязан к ID "unknown".
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	deviceID := flag.String("device-id", "", "")
	flag.String("peer", "", "")
	flag.Bool("startup-config-stdin", false, "")
	flag.Int("n", 0, "")
	flag.String("listen", "", "")
	flag.String("mode", "", "")
	flag.Bool("vkcalls-preflight", false, "")
	flag.Bool("turn-stream-first", false, "")
	flag.Bool("rt-masque", false, "")
	flag.Bool("config-first-start", false, "")
	flag.Bool("hash-fallback", false, "")
	flag.String("captcha-mode", "", "")
	flag.String("device-info", "", "")
	flag.String("turn-sni", "", "")
	flag.Parse()

	in := bufio.NewScanner(os.Stdin)
	in.Scan()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(in.Text(), "START_CONFIG|"))
	if err != nil {
		log.Fatal("bad startup config")
	}
	var secrets map[string]string
	json.Unmarshal(raw, &secrets)

	if secrets["connection_password"] == "crash-once" {
		// Первый запуск выходит сам, как go_client без стартовых кредов.
		if _, err := os.Stat("crashed"); err != nil {
			os.WriteFile("crashed", nil, 0o644)
			log.Printf("[ГРУППА #1] Стартовые креды не получены: CAPTCHA_WAIT_REQUIRED; завершаем запуск для чистого повтора")
			return
		}
	}
	log.Printf("[ГРУППА #1] Креды OK, TURN: [193.203.43.6:19302 10.0.0.1:3478], 9 воркеров")
	if secrets["connection_password"] == "bound" && *deviceID != "unknown" {
		log.Printf("[ВОРКЕР #1] [FAILOVER] Сбой сессии: FATAL_AUTH: пароль привязан к другому устройству (адрес=1.2.3.4:19302, тип=FATAL)")
	} else if secrets["connection_password"] == "wrong" {
		log.Printf("[ВОРКЕР #1] [FAILOVER] Сбой сессии: FATAL_AUTH: неверный пароль подключения (адрес=1.2.3.4:19302, тип=FATAL)")
	} else {
		fmt.Println("CAPTCHA_SOLVE|1-1|auto|https://id.vk.ru/captcha|sess")
		in.Scan()
		log.Printf("[КАПЧА] answer=%s", in.Text())
		fmt.Println()
		fmt.Println("╔══════════════ WireGuard Конфиг ══════════════╗")
		for _, l := range []string{"[Interface]", "PrivateKey = " + *deviceID + "+" + secrets["vk_hashes"], "Address = 10.66.66.2/32", "", "[Peer]", "Endpoint = 127.0.0.1:9000"} {
			fmt.Printf("║ %-44s ║\n", l)
		}
		fmt.Println("╚══════════════════════════════════════════════╝")
		log.Printf("[СТАТИСТИКА] Активных: 9 | ↓1.50 МБ / ↑0.25 МБ")
	}
	for in.Scan() {
		if in.Text() == "STOP" {
			log.Printf("[STDIN] Команда STOP")
			return
		}
	}
}
