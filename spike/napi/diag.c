/*
 * diag.c — diagnostica dei segnali in C puro (non entra mai in Go).
 *
 * Esporta sull'oggetto exports la funzione __sigInfo(signo) che restituisce, per il thread
 * chiamante (il thread JS):
 *   - blocked:     il segnale è nella maschera del thread?
 *   - pending:     il segnale è pendente per il processo/thread?
 *   - disposition: "SIG_DFL" | "SIG_IGN" | "handler"
 *   - owner/symbol: libreria e simbolo dell'handler installato (dladdr), es. node o basic.node
 *   - flags:       SA_ONSTACK, SA_RESETHAND, SA_SIGINFO, SA_RESTART
 * Serve a capire se il runtime Go cambia la gestione dei segnali del processo Node (macOS:
 * SIGTERM con azione di default non termina il processo quando l'addon è caricato).
 */
#if !defined(_WIN32)
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include <dlfcn.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include "gonapi.h"

static napi_status set_str(napi_env env, napi_value obj, const char* key, const char* val) {
  napi_value v;
  napi_status s = gn_napi.napi_create_string_utf8(env, val ? val : "", NAPI_AUTO_LENGTH, &v);
  if (s != napi_ok) return s;
  return gn_napi.napi_set_named_property(env, obj, key, v);
}

static napi_status set_bool(napi_env env, napi_value obj, const char* key, int val) {
  napi_value v;
  napi_status s = gn_napi.napi_get_boolean(env, val != 0, &v);
  if (s != napi_ok) return s;
  return gn_napi.napi_set_named_property(env, obj, key, v);
}

static napi_value gn_diag_siginfo(napi_env env, napi_callback_info info) {
  size_t argc = 1;
  napi_value argv[1];
  int32_t signo = 0;
  if (gn_napi.napi_get_cb_info(env, info, &argc, argv, NULL, NULL) != napi_ok ||
      argc < 1 || gn_napi.napi_get_value_int32(env, argv[0], &signo) != napi_ok ||
      signo <= 0 || signo >= NSIG) {
    gn_napi.napi_throw_type_error(env, NULL, "__sigInfo(signo): numero di segnale non valido");
    return NULL;
  }

  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  sigset_t blocked, pending;
  sigemptyset(&blocked);
  sigemptyset(&pending);
  int sa_ok = sigaction(signo, NULL, &sa) == 0;
  int mask_ok = pthread_sigmask(SIG_BLOCK, NULL, &blocked) == 0;
  int pend_ok = sigpending(&pending) == 0;

  const char* disposition = "unknown";
  const char* owner = "";
  const char* symbol = "";
  char addr[32] = "";
  void* fn = NULL;
  if (sa_ok) {
    if (sa.sa_flags & SA_SIGINFO) fn = (void*)sa.sa_sigaction;
    else fn = (void*)sa.sa_handler;
    if (fn == (void*)SIG_DFL) disposition = "SIG_DFL";
    else if (fn == (void*)SIG_IGN) disposition = "SIG_IGN";
    else {
      disposition = "handler";
      snprintf(addr, sizeof addr, "%p", fn);
      Dl_info di;
      if (dladdr(fn, &di) != 0) {
        owner = di.dli_fname;
        symbol = di.dli_sname;
      }
    }
  }

  napi_value obj;
  if (gn_napi.napi_create_object(env, &obj) != napi_ok) return NULL;
  set_str(env, obj, "disposition", disposition);
  set_str(env, obj, "handler", addr);
  set_str(env, obj, "owner", owner);
  set_str(env, obj, "symbol", symbol);
  set_bool(env, obj, "blocked", mask_ok && sigismember(&blocked, signo) == 1);
  set_bool(env, obj, "pending", pend_ok && sigismember(&pending, signo) == 1);
  set_bool(env, obj, "onstack", sa_ok && (sa.sa_flags & SA_ONSTACK));
  set_bool(env, obj, "resethand", sa_ok && (sa.sa_flags & SA_RESETHAND));
  set_bool(env, obj, "siginfo", sa_ok && (sa.sa_flags & SA_SIGINFO));
  set_bool(env, obj, "restart", sa_ok && (sa.sa_flags & SA_RESTART));
  return obj;
}

napi_status gn_register_diag(napi_env env, napi_value exports) {
  napi_value fn;
  napi_status s = gn_napi.napi_create_function(env, "__sigInfo", NAPI_AUTO_LENGTH, gn_diag_siginfo, NULL, &fn);
  if (s != napi_ok) return s;
  return gn_napi.napi_set_named_property(env, exports, "__sigInfo", fn);
}

#else /* _WIN32: niente segnali POSIX */
#include "gonapi.h"
napi_status gn_register_diag(napi_env env, napi_value exports) {
  (void)env; (void)exports;
  return napi_ok;
}
#endif
