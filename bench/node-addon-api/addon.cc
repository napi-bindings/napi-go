// The node-addon-api implementation of the benchmark addon (bench/README.md): the same functions
// as the napi-go and napi-rs implementations, written the usual way with node-addon-api, with the
// same argument checks (a wrong type throws a TypeError).
#include <napi.h>

#include <cstring>
#include <string>

namespace {

Napi::Value Noop(const Napi::CallbackInfo& info) { return info.Env().Undefined(); }

Napi::Value Add(const Napi::CallbackInfo& info) {
  Napi::Env env = info.Env();
  if (!info[0].IsNumber() || !info[1].IsNumber()) {
    Napi::TypeError::New(env, "numbers expected").ThrowAsJavaScriptException();
    return env.Undefined();
  }
  return Napi::Number::New(env, info[0].As<Napi::Number>().DoubleValue() + info[1].As<Napi::Number>().DoubleValue());
}

Napi::Value Echo(const Napi::CallbackInfo& info) {
  Napi::Env env = info.Env();
  if (!info[0].IsString()) {
    Napi::TypeError::New(env, "string expected").ThrowAsJavaScriptException();
    return env.Undefined();
  }
  std::string s = info[0].As<Napi::String>().Utf8Value();
  return Napi::String::New(env, s);
}

Napi::Value BufferLength(const Napi::CallbackInfo& info) {
  Napi::Env env = info.Env();
  if (!info[0].IsTypedArray()) {
    Napi::TypeError::New(env, "Uint8Array expected").ThrowAsJavaScriptException();
    return env.Undefined();
  }
  return Napi::Number::New(env, static_cast<double>(info[0].As<Napi::Uint8Array>().ByteLength()));
}

Napi::Value MakeBuffer(const Napi::CallbackInfo& info) {
  Napi::Env env = info.Env();
  if (!info[0].IsNumber()) {
    Napi::TypeError::New(env, "number expected").ThrowAsJavaScriptException();
    return env.Undefined();
  }
  size_t n = info[0].As<Napi::Number>().Uint32Value();
  // napi_create_buffer does not clear the memory: clear it, as the other implementations do.
  Napi::Buffer<uint8_t> buf = Napi::Buffer<uint8_t>::New(env, n);
  std::memset(buf.Data(), 0, n);
  return buf;
}

// ResolveWorker resolves a Promise with n from the libuv thread pool.
class ResolveWorker : public Napi::AsyncWorker {
 public:
  ResolveWorker(Napi::Env env, double n) : Napi::AsyncWorker(env), deferred_(Napi::Promise::Deferred::New(env)), n_(n) {}
  Napi::Promise Promise() { return deferred_.Promise(); }

 protected:
  void Execute() override {}
  void OnOK() override { deferred_.Resolve(Napi::Number::New(Env(), n_)); }
  void OnError(const Napi::Error& err) override { deferred_.Reject(err.Value()); }

 private:
  Napi::Promise::Deferred deferred_;
  double n_;
};

Napi::Value ResolveAsync(const Napi::CallbackInfo& info) {
  Napi::Env env = info.Env();
  if (!info[0].IsNumber()) {
    Napi::TypeError::New(env, "number expected").ThrowAsJavaScriptException();
    return env.Undefined();
  }
  auto* worker = new ResolveWorker(env, info[0].As<Napi::Number>().DoubleValue());
  Napi::Promise promise = worker->Promise();
  worker->Queue();
  return promise;
}

Napi::Object Init(Napi::Env env, Napi::Object exports) {
  exports.Set("noop", Napi::Function::New(env, Noop, "noop"));
  exports.Set("add", Napi::Function::New(env, Add, "add"));
  exports.Set("echo", Napi::Function::New(env, Echo, "echo"));
  exports.Set("bufferLength", Napi::Function::New(env, BufferLength, "bufferLength"));
  exports.Set("makeBuffer", Napi::Function::New(env, MakeBuffer, "makeBuffer"));
  exports.Set("resolveAsync", Napi::Function::New(env, ResolveAsync, "resolveAsync"));
  return exports;
}

}  // namespace

NODE_API_MODULE(bench_node_addon_api, Init)
