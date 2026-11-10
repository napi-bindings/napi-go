/*
 * typed.c — typed callback trampoline (Layer 2 of napi-go).
 *
 * A function created with gn_typed_callback carries a gn_typed_sig (C memory) as its data: the
 * kind expected for each argument. The trampoline converts numbers, booleans and short strings
 * in C, so that entering Go is the only cgo transition on the common path, and creates the result
 * in C from what Go wrote in gn_typed_out. A conversion that fails in C (wrong JS type, string too
 * long for the buffer) leaves the argument as a napi_value: Go converts it again and reports the
 * error with the same message as the untyped path.
 */
#include <stdlib.h>
#include "internal.h"
#include "_cgo_export.h"

/* The signature is returned as an integer: Go keeps it as a uintptr (passed to Node-API as data),
   so the pointer conversions happen here and not in Go. */
uintptr_t gn_typed_sig_new(uintptr_t id, int32_t flags, int32_t nargs, const int32_t* kinds) {
  if (nargs < 0 || nargs > GN_INLINE_ARGS) return 0;
  gn_typed_sig* s = (gn_typed_sig*)calloc(1, sizeof(gn_typed_sig));
  if (s == NULL) return 0;
  s->id = id;
  s->flags = flags;
  s->nargs = nargs;
  for (int32_t i = 0; i < nargs; i++) s->kinds[i] = kinds[i];
  return (uintptr_t)s;
}

void gn_typed_sig_free(uintptr_t s) { free((void*)s); }

/* UTF-8 truncation: napi_get_value_string_utf8 never splits a character, so with a buffer of
   size n it may stop up to 3 bytes before n - 1. Only a copy shorter than this is surely complete. */
#define GN_UTF8_SLACK 4

napi_value gn_typed_callback(napi_env env, napi_callback_info info) {
  size_t argc = GN_INLINE_ARGS;
  napi_value argv[GN_INLINE_ARGS];
  gn_r_napi_get_cb_info r = gn_napi_get_cb_info(env, info, &argc, argv, 1u | 2u);
  if (r.status != napi_ok) return NULL;
  const gn_typed_sig* sig = (const gn_typed_sig*)r.o1;
  gn_typed_arg args[GN_INLINE_ARGS];
  char sbuf[GN_TYPED_STRBUF]; /* input strings, shared */
  size_t used = 0;
  size_t n = argc < (size_t)sig->nargs ? argc : (size_t)sig->nargs;
  if (n > GN_INLINE_ARGS) n = GN_INLINE_ARGS;
  for (size_t i = 0; i < n; i++) {
    gn_typed_arg* a = &args[i];
    a->kind = GN_TK_VALUE;
    a->v = argv[i];
    switch (sig->kinds[i]) {
      case GN_TK_F64: {
        gn_r_napi_get_value_double d = gn_napi_get_value_double(env, argv[i], 1u);
        if (d.status == napi_ok) {
          a->kind = GN_TK_F64;
          a->f64 = d.o0;
        }
        break;
      }
      case GN_TK_BOOL: {
        gn_r_napi_get_value_bool b = gn_napi_get_value_bool(env, argv[i], 1u);
        if (b.status == napi_ok) {
          a->kind = GN_TK_BOOL;
          a->b = b.o0 ? 1 : 0;
        }
        break;
      }
      case GN_TK_STR: {
        size_t avail = GN_TYPED_STRBUF - used;
        if (avail <= GN_UTF8_SLACK + 1) break;
        gn_r_napi_get_value_string_utf8 s = gn_napi_get_value_string_utf8(env, argv[i], sbuf + used, avail, 1u);
        if (s.status != napi_ok || s.o0 + GN_UTF8_SLACK >= avail - 1) break; /* not a string, or maybe cut */
        a->kind = GN_TK_STR;
        a->sp = sbuf + used;
        a->sn = s.o0;
        used += s.o0 + 1;
        break;
      }
      default:
        break;
    }
  }
  /* Methods: the native object of this (0 if this wraps nothing; Go then reports the error). */
  uintptr_t wrapped = 0;
  if (sig->flags & GN_TF_UNWRAP_THIS) {
    gn_r_napi_unwrap u = gn_napi_unwrap(env, r.o0, 1u);
    if (u.status == napi_ok) wrapped = (uintptr_t)u.o0;
  }
  if (!gn_enter()) {
    gn_throw_foreign_runtime(env);
    return NULL;
  }
  char obuf[GN_TYPED_STRBUF];
  gn_typed_out out;
  out.kind = GN_TK_VALUE;
  out.buf = obuf;
  out.cap = sizeof obuf;
  out.sn = 0;
  napi_value ret = goNapiTypedCallback(env, info, sig->id, argc, argv, r.o0, wrapped, args, n, &out);
  gn_leave();
  switch (out.kind) {
    case GN_TK_F64: return gn_napi_create_double(env, out.f64, 1u).o0;
    case GN_TK_I32: return gn_napi_create_int32(env, out.i32, 1u).o0;
    case GN_TK_U32: return gn_napi_create_uint32(env, out.u32, 1u).o0;
    case GN_TK_BOOL: return gn_napi_get_boolean(env, out.b != 0, 1u).o0;
    case GN_TK_STR: return gn_napi_create_string_utf8(env, obuf, out.sn, 1u).o0;
    default: return ret;
  }
}
