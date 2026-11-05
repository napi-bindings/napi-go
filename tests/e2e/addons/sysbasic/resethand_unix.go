//go:build !windows

package main

/*
#include <signal.h>
#include <string.h>

// clear_resethand reinstalls the handler of signo without SA_RESETHAND. Returns 0 on success.
static int clear_resethand(int signo) {
  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  if (sigaction(signo, NULL, &sa) != 0) return -1;
  sa.sa_flags &= ~SA_RESETHAND;
  return sigaction(signo, &sa, NULL);
}
*/
import "C"

import "errors"

func clearResetHand(signo int) error {
	if C.clear_resethand(C.int(signo)) != 0 {
		return errors.New("sigaction failed")
	}
	return nil
}
