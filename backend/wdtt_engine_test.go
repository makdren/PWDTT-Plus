package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	core "wg-turn-client"
)

func buildFakeClient(t *testing.T) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "fake.exe")
	cmd := exec.Command("go", "build", "-o", out, "./testdata/fakeclient")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake client: %v\n%s", err, msg)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type engineRun struct {
	events []core.Event
	turn   []string
}

func runFakeEngine(t *testing.T, exe []byte, opts engineOptions, stopAfterConfig bool) engineRun {
	t.Helper()
	old := wdttEngineExe
	wdttEngineExe = exe
	defer func() { wdttEngineExe = old }()

	var mu sync.Mutex
	var run engineRun
	e, events, err := startWDTTEngine(t.TempDir(), opts, func(ips []string) {
		mu.Lock()
		run.turn = append(run.turn, ips...)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	timeout := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				time.Sleep(50 * time.Millisecond) // фоновые excludeNew
				mu.Lock()
				defer mu.Unlock()
				return run
			}
			run.events = append(run.events, ev)
			if stopAfterConfig && ev.Type == core.EventStats {
				e.Stop()
			}
		case <-timeout:
			e.Stop()
			t.Fatal("engine did not finish")
		}
	}
}

func findEvent(evs []core.Event, typ core.EventType, name string) *core.Event {
	for i := range evs {
		if evs[i].Type == typ && (name == "" || evs[i].Name == name) {
			return &evs[i]
		}
	}
	return nil
}

func TestWDTTEngineConfigCaptchaStatsStop(t *testing.T) {
	exe := buildFakeClient(t)
	run := runFakeEngine(t, exe, engineOptions{
		Peer: "1.2.3.4:56000", Password: "pw", Hashes: []string{"h1", "h2"},
		Workers: 18, Listen: "127.0.0.1:9000", DeviceID: "pwdtt-abc",
	}, true)

	cfg := findEvent(run.events, core.EventEvent, "wg_config")
	if cfg == nil {
		t.Fatalf("no wg_config event: %+v", run.events)
	}
	want := "[Interface]\nPrivateKey = pwdtt-abc+h1,h2\nAddress = 10.66.66.2/32\n[Peer]\nEndpoint = 127.0.0.1:9000"
	if cfg.Data != want {
		t.Fatalf("config:\n%q\nwant\n%q", cfg.Data, want)
	}
	st := findEvent(run.events, core.EventStats, "")
	if st == nil || st.Active != 9 || st.BytesDown != 1572864 || st.BytesUp != 262144 {
		t.Fatalf("stats event: %+v", st)
	}
	var captcha, stop bool
	for _, ev := range run.events {
		captcha = captcha || strings.Contains(ev.Message, "answer=CAPTCHA_RESULT|1-1|error:unsupported")
		stop = stop || strings.Contains(ev.Message, "Команда STOP")
	}
	if !captcha || !stop {
		t.Fatalf("captcha answered=%v stop delivered=%v", captcha, stop)
	}
	if !reflect.DeepEqual(run.turn, []string{"193.203.43.6"}) {
		t.Fatalf("TURN excluded: %v", run.turn)
	}
	if findEvent(run.events, core.EventError, "") != nil {
		t.Fatalf("unexpected error event: %+v", run.events)
	}
}

func TestWDTTEngineRetriesLegacyDeviceID(t *testing.T) {
	exe := buildFakeClient(t)
	run := runFakeEngine(t, exe, engineOptions{
		Peer: "1.2.3.4:56000", Password: "bound", Hashes: []string{"h1"},
		Workers: 9, Listen: "127.0.0.1:9000", DeviceID: "pwdtt-abc",
	}, true)
	cfg := findEvent(run.events, core.EventEvent, "wg_config")
	if cfg == nil || !strings.Contains(cfg.Data, "PrivateKey = unknown+h1") {
		t.Fatalf("expected config with legacy ID, events: %+v", run.events)
	}
	if ev := findEvent(run.events, core.EventError, ""); ev != nil {
		t.Fatalf("unexpected error: %s", ev.Message)
	}
}

func TestWDTTEngineStopsOnDenied(t *testing.T) {
	exe := buildFakeClient(t)
	run := runFakeEngine(t, exe, engineOptions{
		Peer: "1.2.3.4:56000", Password: "wrong", Hashes: []string{"h1"},
		Workers: 9, Listen: "127.0.0.1:9000", DeviceID: "pwdtt-abc",
	}, false)
	ev := findEvent(run.events, core.EventError, "")
	if ev == nil || ev.Message != "Сервер отказал в доступе: неверный пароль подключения" {
		t.Fatalf("error event: %+v", ev)
	}
	if findEvent(run.events, core.EventEvent, "wg_config") != nil {
		t.Fatal("no config expected")
	}
}

func TestEngineArgsUseExplicitBooleans(t *testing.T) {
	args := engineOptions{Peer: "p:1", Workers: 54, Listen: "127.0.0.1:9000", DeviceID: "d",
		StreamOnly: true, TurnSNI: "vk.com", Hashes: []string{"a", "b"}}.args("d")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-turn-stream-first=true", "-hash-fallback=true", "-turn-sni vk.com", "-n 54", "-device-id d", "-rt-masque=false"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q lack %q", joined, want)
		}
	}
}

func TestPublicIPv4s(t *testing.T) {
	got := publicIPv4s("Relay: 193.203.43.6:42083; peer: 31.76.240.105:56000 via 10.0.0.1 127.0.0.1 100.64.1.1 192.168.1.1")
	if !reflect.DeepEqual(got, []string{"193.203.43.6", "31.76.240.105"}) {
		t.Fatalf("got %v", got)
	}
}

func TestWDTTEngineRestartsAfterClientExit(t *testing.T) {
	exe := buildFakeClient(t)
	run := runFakeEngine(t, exe, engineOptions{
		Peer: "1.2.3.4:56000", Password: "crash-once", Hashes: []string{"h1"},
		Workers: 9, Listen: "127.0.0.1:9000", DeviceID: "pwdtt-abc",
	}, true)
	if findEvent(run.events, core.EventEvent, "wg_config") == nil {
		t.Fatalf("no config after restart: %+v", run.events)
	}
	var restarted bool
	for _, ev := range run.events {
		restarted = restarted || strings.Contains(ev.Message, "перезапуск через 2s")
	}
	if !restarted {
		t.Fatal("restart was not logged")
	}
}
