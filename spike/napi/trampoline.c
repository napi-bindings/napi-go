/*
 * trampoline.c — unici punti di ingresso C → Go.
 *
 * Tutte le funzioni JS create da napi-go puntano a gn_callback: il `data` della
 * funzione è un indice nel registro Go. napi_get_cb_info viene chiamata qui,
 * prima di entrare in Go, così ogni chiamata JS→Go attraversa cgo una volta sola
 * per argc/argv/this/data.
 */
#include "gonapi.h"
#include "_cgo_export.h"

napi_value gn_callback(napi_env env, napi_callback_info info) {
  size_t argc = GN_INLINE_ARGS;
  napi_value argv[GN_INLINE_ARGS];
  napi_value self = NULL;
  void* data = NULL;
  if (gn_napi.napi_get_cb_info(env, info, &argc, argv, &self, &data) != napi_ok) return NULL;
  /* Gli slot argv[argc..GN_INLINE_ARGS) sono riempiti da Node con `undefined`. */
  return goNapiCallback(env, info, (uintptr_t)data, argc, argv, self);
}

napi_value gn_typed_callback(napi_env env, napi_callback_info info) {
  size_t argc = GN_INLINE_ARGS;
  napi_value argv[GN_INLINE_ARGS];
  void* data = NULL;
  if (gn_napi.napi_get_cb_info(env, info, &argc, argv, NULL, &data) != napi_ok) return NULL;
  const gn_sig* sig = (const gn_sig*)data;
  gn_arg args[GN_INLINE_ARGS];
  char sbuf[GN_STRBUF]; /* stringhe in ingresso: buffer condiviso sullo stack */
  size_t used = 0;
  for (int32_t i = 0; i < sig->nargs; i++) {
    args[i].kind = sig->kinds[i];
    switch (sig->kinds[i]) {
      case GN_K_F64:
        if (gn_napi.napi_get_value_double(env, argv[i], &args[i].u.f64) != napi_ok) goto number_expected;
        break;
      case GN_K_I32:
        if (gn_napi.napi_get_value_int32(env, argv[i], &args[i].u.i32) != napi_ok) goto number_expected;
        break;
      case GN_K_BOOL:
        if (gn_napi.napi_get_value_bool(env, argv[i], &args[i].u.b) != napi_ok) {
          gn_napi.napi_throw_type_error(env, NULL, "A boolean was expected");
          return NULL;
        }
        break;
      case GN_K_STR: {
        size_t len = 0;
        if (gn_napi.napi_get_value_string_utf8(env, argv[i], NULL, 0, &len) != napi_ok) {
          gn_napi.napi_throw_type_error(env, NULL, "A string was expected");
          return NULL;
        }
        if (len + 1 <= GN_STRBUF - used) {
          size_t w = 0;
          gn_napi.napi_get_value_string_utf8(env, argv[i], sbuf + used, len + 1, &w);
          args[i].u.s.p = sbuf + used;
          args[i].u.s.n = w;
          used += w + 1;
        } else { /* troppo lunga: Go la leggerà dal napi_value */
          args[i].kind = GN_K_VALUE;
          args[i].u.v = argv[i];
        }
        break;
      }
      default:
        args[i].kind = GN_K_VALUE;
        args[i].u.v = argv[i];
    }
  }
  char obuf[GN_STRBUF];
  gn_arg out;
  out.kind = sig->ret;
  out.u.s.p = obuf; /* per GN_K_STR: Go copia qui il risultato (capacità in n) */
  out.u.s.n = sizeof obuf;
  if (goNapiTypedCallback(env, sig->id, args, sig->nargs, &out) != 0) return NULL; /* Go ha già lanciato */
  napi_value r = NULL;
  switch (out.kind) {
    case GN_K_F64: gn_napi.napi_create_double(env, out.u.f64, &r); break;
    case GN_K_I32: gn_napi.napi_create_int32(env, out.u.i32, &r); break;
    case GN_K_BOOL: gn_napi.napi_get_boolean(env, out.u.b, &r); break;
    case GN_K_STR: gn_napi.napi_create_string_utf8(env, out.u.s.p, out.u.s.n, &r); break;
    case GN_K_VALUE: r = out.u.v; break;
    default: break;
  }
  return r;
number_expected:
  gn_napi.napi_throw_type_error(env, NULL, "A number was expected");
  return NULL;
}

/* call_js del dispatcher: eseguita sul thread JS dell'env. Con env == NULL la TSFN
   sta venendo distrutta (teardown): Go deve solo scartare la coda. */
void gn_tsfn_call_js(napi_env env, napi_value js_cb, void* ctx, void* data) {
  (void)js_cb; (void)data;
  goNapiDispatch(env, (uintptr_t)ctx);
}

void gn_env_cleanup(void* arg) {
  goNapiEnvCleanup((uintptr_t)arg);
}
