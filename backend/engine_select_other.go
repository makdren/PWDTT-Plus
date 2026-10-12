//go:build !windows

package backend

import core "wg-turn-client"

// startTunnelEngine вне Windows — встроенное ядро PWDTT.
func startTunnelEngine(_ *Store, cfg core.Config) (tunnelEngine, <-chan core.Event, error) {
	c := core.New(cfg)
	events, err := c.Start()
	if err != nil {
		c.Stop()
		return nil, nil, err
	}
	return c, events, nil
}
