//go:build !windows

package backend

import "os"

func raisePriority(*os.Process) {}
