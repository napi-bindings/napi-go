/* musl host that loads lib.so via dlopen after startup, just as Node does with a .node addon. */
#include <dlfcn.h>
#include <stdio.h>
int main(int argc, char** argv) {
  const char* path = argc > 1 ? argv[1] : "./lib.so";
  void* h = dlopen(path, RTLD_NOW);
  if (!h) { printf("dlopen FAILED: %s\n", dlerror()); return 1; }
  int (*f)(void) = (int (*)(void))dlsym(h, "GoAnswer");
  if (!f) { printf("dlsym FAILED: %s\n", dlerror()); return 2; }
  printf("dlopen OK, GoAnswer() = %d\n", f());
  return 0;
}
