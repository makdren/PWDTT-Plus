package backend

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLogBatching(t *testing.T) {
	var mu sync.Mutex
	var events []string
	var lines int
	b := NewBridge(context.Background(), nil, func(name string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, name)
		if name == "log_batch" {
			batch := args[0].([][2]string)
			lines += len(batch)
			if batch[0][1] != "line 0" {
				t.Errorf("order broken: %v", batch[0])
			}
		}
	})
	for i := 0; i < 100; i++ {
		b.queueLog("INFO", "line "+itoaTest(i))
	}
	time.Sleep(2 * logFlushDelay)
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || lines != 100 {
		t.Fatalf("want 1 batch of 100 lines, got events=%v lines=%d", events, lines)
	}
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}
