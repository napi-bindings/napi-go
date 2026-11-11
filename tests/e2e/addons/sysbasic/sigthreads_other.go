//go:build !linux

package main

import (
	"errors"
	"time"
)

// errLinuxOnly: tgkill and /proc/self/task are Linux interfaces.
var errLinuxOnly = errors.New("Linux only")

func signalAllThreads(int) (int, error) { return 0, errLinuxOnly }

func handlerFlags(int) (int64, error) { return 0, errLinuxOnly }

const saOnStack = int64(0)

// startWatchdog is a no-op outside Linux (the tests using it are Linux only).
func startWatchdog(time.Duration) error { return nil }
