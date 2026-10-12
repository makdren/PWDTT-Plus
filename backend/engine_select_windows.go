//go:build windows

package backend

import core "wg-turn-client"

// startTunnelEngine на Windows запускает официальный клиент WDTT Plus.
func startTunnelEngine(store *Store, cfg core.Config) (tunnelEngine, <-chan core.Event, error) {
	listen := cfg.Listen
	if listen == "" {
		listen = "127.0.0.1:9000"
	}
	opts := engineOptions{
		Peer:        cfg.PeerAddr,
		Password:    cfg.Password,
		Hashes:      cfg.Hashes,
		Workers:     cfg.Workers,
		Listen:      listen,
		DeviceID:    cfg.DeviceID,
		DeviceInfo:  deviceInfoJSON(Version),
		StreamOnly:  cfg.TurnMode == core.ConnStream,
		TurnSNI:     cfg.TurnSNI,
		Fingerprint: cfg.Fingerprint,
	}
	e, events, err := startWDTTEngine(store.baseDir, opts, cfg.ExcludeTURN)
	if err != nil {
		return nil, nil, err
	}
	return e, events, nil
}
