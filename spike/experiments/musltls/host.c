/* Host musl che carica lib.so con dlopen dopo l'avvio, come fa Node con un .node. */
#include <dlfcn.h>
#include <stdio.h>
int main(int argc, char** argv) {
  const char* path = argc > 1 ? argv[1] : "./lib.so";
  void* h = dlopen(path, RTLD_NOW);
  if (!h) { printf("dlopen FALLITO: %s\n", dlerror()); return 1; }
  int (*f)(void) = (int (*)(void))dlsym(h, "GoAnswer");
  if (!f) { printf("dlsym FALLITO: %s\n", dlerror()); return 2; }
  printf("dlopen OK, GoAnswer() = %d\n", f());
  return 0;
}
