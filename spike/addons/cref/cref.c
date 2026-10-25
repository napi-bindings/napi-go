/* Addon Node-API in C puro: baseline per misurare l'overhead aggiunto da cgo.
 * Build (Unix): cc -O2 -shared -fPIC -I../../napi/include cref.c -o cref.node
 *               (macOS: aggiungere -undefined dynamic_lookup)
 */
#include <node_api.h>
#include <stdlib.h>
#include <string.h>

static napi_value Noop(napi_env env, napi_callback_info info) { return NULL; }

static napi_value Add(napi_env env, napi_callback_info info) {
  size_t argc = 2; napi_value argv[2]; double a, b; napi_value r;
  napi_get_cb_info(env, info, &argc, argv, NULL, NULL);
  napi_get_value_double(env, argv[0], &a);
  napi_get_value_double(env, argv[1], &b);
  napi_create_double(env, a + b, &r);
  return r;
}

static napi_value Echo(napi_env env, napi_callback_info info) {
  size_t argc = 1; napi_value argv[1]; size_t len; napi_value r; char sbuf[256];
  napi_get_cb_info(env, info, &argc, argv, NULL, NULL);
  napi_get_value_string_utf8(env, argv[0], NULL, 0, &len);
  char* buf = len < sizeof sbuf ? sbuf : malloc(len + 1);
  napi_get_value_string_utf8(env, argv[0], buf, len + 1, &len);
  napi_create_string_utf8(env, buf, len, &r);
  if (buf != sbuf) free(buf);
  return r;
}

NAPI_MODULE_INIT() {
  napi_property_descriptor d[] = {
    {"noop", NULL, Noop, NULL, NULL, NULL, napi_default, NULL},
    {"add", NULL, Add, NULL, NULL, NULL, napi_default, NULL},
    {"echo", NULL, Echo, NULL, NULL, NULL, napi_default, NULL},
  };
  napi_define_properties(env, exports, sizeof d / sizeof d[0], d);
  return exports;
}
