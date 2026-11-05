//go:build windows

package main

import "errors"

// clearResetHand is POSIX only: Windows has no sigaction.
func clearResetHand(int) error { return errors.New("not supported on Windows") }
