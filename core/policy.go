package core

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// Совместимость с сервером WDTT-Plus.
//
// Сервер WDTT-Plus может ограничить число одновременных DTLS-сессий на доступ.
// Сессия сверх лимита получает "POLICY:max_workers=N" и закрывается. Без
// обработки такой воркер бесконечно переподключался бы, поэтому ядро запоминает
// лимит и гасит воркеры с номером больше N.

const workerPolicyPrefix = "POLICY:max_workers="

// serverWorkerLimit — последний лимит, присланный сервером (0 — без лимита).
var serverWorkerLimit atomic.Int32

func resetServerWorkerLimit() { serverWorkerLimit.Store(0) }

// parseWorkerPolicy разбирает ответ POLICY сервера WDTT-Plus.
func parseWorkerPolicy(resp string) (int, bool) {
	value, ok := strings.CutPrefix(strings.TrimSpace(resp), workerPolicyPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 || n > 128 {
		return 0, false
	}
	return n, true
}

// noteWorkerPolicy сохраняет лимит; возвращает true, если он изменился.
func noteWorkerPolicy(limit int) bool {
	return serverWorkerLimit.Swap(int32(limit)) != int32(limit)
}

// workerOverServerLimit — воркер с этим номером превышает лимит сервера.
func workerOverServerLimit(workerID int) bool {
	limit := int(serverWorkerLimit.Load())
	return limit > 0 && workerID > limit
}

type workerPolicyError struct{ maxWorkers int }

func (e *workerPolicyError) Error() string {
	return fmt.Sprintf("сервер ограничил мощность доступа до %d потоков", e.maxWorkers)
}

// stripServiceConfigLines убирает служебные строки WDTT-Plus ("# WDTT_TRANSPORTS=…")
// из WireGuard-конфига: они нужны только мультипутевому транспорту Android-клиента.
func stripServiceConfigLines(conf string) string {
	lines := strings.Split(conf, "\n")
	out := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# WDTT_") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
