/*
 * onstack.c — makes the signal handlers that Node.js installs after the module is loaded run on
 * the alternate signal stack (SA_ONSTACK), as the Go runtime requires.
 *
 * Problem (observed on linux/arm64 CI, reproduced on linux/amd64, Go 1.24–1.27, Node 22–27):
 * libuv installs its handlers (uv__signal_register_handler: SIGCHLD on the first child process,
 * every signal with a JS listener, SIGWINCH for TTYs) with SA_RESTART only. A process-directed
 * signal can be delivered to any thread that does not block it, Go threads included, and without
 * SA_ONSTACK the kernel writes the signal frame below the stack pointer of whatever is running:
 * on a goroutine stack (a few KiB; the frame is about 1–3 KiB on x86-64 and 4.6 KiB or more on
 * arm64) it overruns the stack bounds and overwrites the neighbouring goroutine stacks. The
 * symptoms are random: "fatal error: traceback did not unwind completely", sigpanic in runtime
 * functions with garbage arguments, nil dereferences at small addresses. The same holds on the JS
 * thread while it runs Go code (a callback runs on a goroutine stack). The Go documentation
 * (os/signal, "Go programs that use cgo or SWIG") requires non-Go handlers to use SA_ONSTACK;
 * the Go runtime adds it to the handlers that exist when it starts, not to later ones.
 *
 * Fix: when the module loads, every sigaction slot in the global offset table of the objects
 * already loaded (the node executable, or libuv/libnode when they are shared libraries) is
 * redirected to gn_sigaction_onstack, which adds SA_ONSTACK to every real handler (not SIG_DFL
 * or SIG_IGN) and calls the real sigaction. A thread with no alternate stack is unaffected
 * (the kernel then uses the current stack, as before); Go threads always have one.
 *
 * Scope: Linux on the architectures listed below, glibc and musl. Elsewhere it is a no-op:
 * Windows has no signals; macOS uses a different binary format (Mach-O), see the
 * implementation plan. Objects loaded after the module are not patched.
 */
#if !defined(_WIN32) && !defined(_GNU_SOURCE)
#define _GNU_SOURCE /* dl_iterate_phdr on glibc: must be defined before any include */
#endif
#include "internal.h"

#if defined(__linux__) && (defined(__x86_64__) || defined(__aarch64__) || defined(__i386__) || \
                           defined(__arm__) || defined(__riscv) || defined(__s390x__))
#include <link.h>
#include <pthread.h>
#include <signal.h>
#include <stdint.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>

#if defined(__x86_64__)
#define GN_R_JUMP_SLOT R_X86_64_JUMP_SLOT
#define GN_R_GLOB_DAT R_X86_64_GLOB_DAT
#elif defined(__aarch64__)
#define GN_R_JUMP_SLOT R_AARCH64_JUMP_SLOT
#define GN_R_GLOB_DAT R_AARCH64_GLOB_DAT
#elif defined(__i386__)
#define GN_R_JUMP_SLOT R_386_JMP_SLOT
#define GN_R_GLOB_DAT R_386_GLOB_DAT
#elif defined(__arm__)
#define GN_R_JUMP_SLOT R_ARM_JUMP_SLOT
#define GN_R_GLOB_DAT R_ARM_GLOB_DAT
#elif defined(__riscv)
#define GN_R_JUMP_SLOT R_RISCV_JUMP_SLOT
#define GN_R_GLOB_DAT R_RISCV_JUMP_SLOT /* RISC-V has no GLOB_DAT */
#elif defined(__s390x__)
#define GN_R_JUMP_SLOT R_390_JMP_SLOT
#define GN_R_GLOB_DAT R_390_GLOB_DAT
#endif

#if __SIZEOF_POINTER__ == 8
#define GN_R_SYM(i) ELF64_R_SYM(i)
#define GN_R_TYPE(i) ELF64_R_TYPE(i)
#else
#define GN_R_SYM(i) ELF32_R_SYM(i)
#define GN_R_TYPE(i) ELF32_R_TYPE(i)
#endif

static const char gn_hooked_symbol[] = "sigaction";

/* The replacement. The sigaction it calls is resolved through this module's own GOT, which is
   never patched, so it reaches libc (or another module's replacement, which reaches libc). */
static int gn_sigaction_onstack(int signo, const struct sigaction* act, struct sigaction* old) {
  if (act != NULL && (act->sa_flags & SA_ONSTACK) == 0 && act->sa_handler != SIG_DFL &&
      act->sa_handler != SIG_IGN) {
    struct sigaction copy = *act;
    copy.sa_flags |= SA_ONSTACK;
    return sigaction(signo, &copy, old);
  }
  return sigaction(signo, act, old);
}

/* The tables of one object needed to find its sigaction relocations. */
struct gn_dyn {
  uintptr_t bias;
  const ElfW(Sym)* symtab;
  const char* strtab;
  size_t strsz;
  uintptr_t relro_start, relro_end;
};

/* Dynamic-section addresses are relocated by glibc but not by musl (nor by glibc on RISC-V, where
   the section is read-only): an address below the load bias is still relative. */
static uintptr_t gn_dyn_addr(uintptr_t bias, ElfW(Addr) ptr) {
  return (uintptr_t)ptr < bias ? bias + (uintptr_t)ptr : (uintptr_t)ptr;
}

static void gn_patch_slot(const struct gn_dyn* d, void** slot) {
  void* hook = (void*)gn_sigaction_onstack;
  if (__atomic_load_n(slot, __ATOMIC_ACQUIRE) == hook) return;
  uintptr_t addr = (uintptr_t)slot;
  int relro = addr >= d->relro_start && addr < d->relro_end;
  long page_size = sysconf(_SC_PAGESIZE);
  if (page_size <= 0) return;
  void* page = (void*)(addr & ~((uintptr_t)page_size - 1));
  /* RELRO pages are read-only once relocated; elsewhere the GOT is writable. If mprotect fails,
     leave the slot alone: the process keeps working as without the fix. */
  if (relro && mprotect(page, (size_t)page_size, PROT_READ | PROT_WRITE) != 0) return;
  __atomic_store_n(slot, hook, __ATOMIC_RELEASE);
  /* If restoring fails the page merely stays writable: hardening is lost, nothing breaks. */
  if (relro) (void)mprotect(page, (size_t)page_size, PROT_READ);
}

static int gn_is_hooked_symbol(const struct gn_dyn* d, size_t sym) {
  size_t name = d->symtab[sym].st_name;
  return name < d->strsz && strcmp(d->strtab + name, gn_hooked_symbol) == 0;
}

/* entry_size is the size of one relocation (Rela or Rel); r_offset and r_info lead both. */
static void gn_patch_table(const struct gn_dyn* d, uintptr_t table, size_t size, size_t entry_size) {
  if (table == 0 || entry_size == 0) return;
  for (size_t off = 0; off + entry_size <= size; off += entry_size) {
    const ElfW(Rel)* r = (const ElfW(Rel)*)(table + off);
    unsigned long type = (unsigned long)GN_R_TYPE(r->r_info);
    if (type != GN_R_JUMP_SLOT && type != GN_R_GLOB_DAT) continue;
    size_t sym = (size_t)GN_R_SYM(r->r_info);
    if (sym != 0 && gn_is_hooked_symbol(d, sym)) gn_patch_slot(d, (void**)(d->bias + r->r_offset));
  }
}

/* Whether addr lies in one of the loadable segments of the object. */
static int gn_contains(const struct dl_phdr_info* info, uintptr_t addr) {
  for (int i = 0; i < info->dlpi_phnum; i++) {
    const ElfW(Phdr)* ph = &info->dlpi_phdr[i];
    uintptr_t start = info->dlpi_addr + ph->p_vaddr;
    if (ph->p_type == PT_LOAD && addr >= start && addr < start + ph->p_memsz) return 1;
  }
  return 0;
}

static int gn_patch_object(struct dl_phdr_info* info, size_t size, void* unused) {
  (void)size;
  (void)unused;
  /* Not this module: its own sigaction must keep reaching libc. */
  if (gn_contains(info, (uintptr_t)gn_sigaction_onstack)) return 0;
  struct gn_dyn d;
  memset(&d, 0, sizeof d);
  d.bias = info->dlpi_addr;
  const ElfW(Dyn)* dyn = NULL;
  for (int i = 0; i < info->dlpi_phnum; i++) {
    const ElfW(Phdr)* ph = &info->dlpi_phdr[i];
    if (ph->p_type == PT_DYNAMIC) dyn = (const ElfW(Dyn)*)(d.bias + ph->p_vaddr);
    if (ph->p_type == PT_GNU_RELRO) {
      d.relro_start = d.bias + ph->p_vaddr;
      d.relro_end = d.relro_start + ph->p_memsz;
    }
  }
  if (dyn == NULL) return 0;
  uintptr_t jmprel = 0, rela = 0, rel = 0;
  size_t pltrelsz = 0, relasz = 0, relsz = 0, relaent = sizeof(ElfW(Rela)), relent = sizeof(ElfW(Rel));
  long pltrel = DT_NULL;
  for (; dyn->d_tag != DT_NULL; dyn++) {
    switch (dyn->d_tag) {
      case DT_SYMTAB: d.symtab = (const ElfW(Sym)*)gn_dyn_addr(d.bias, dyn->d_un.d_ptr); break;
      case DT_STRTAB: d.strtab = (const char*)gn_dyn_addr(d.bias, dyn->d_un.d_ptr); break;
      case DT_STRSZ: d.strsz = (size_t)dyn->d_un.d_val; break;
      case DT_JMPREL: jmprel = gn_dyn_addr(d.bias, dyn->d_un.d_ptr); break;
      case DT_PLTRELSZ: pltrelsz = (size_t)dyn->d_un.d_val; break;
      case DT_PLTREL: pltrel = (long)dyn->d_un.d_val; break;
      case DT_RELA: rela = gn_dyn_addr(d.bias, dyn->d_un.d_ptr); break;
      case DT_RELASZ: relasz = (size_t)dyn->d_un.d_val; break;
      case DT_RELAENT: relaent = (size_t)dyn->d_un.d_val; break;
      case DT_REL: rel = gn_dyn_addr(d.bias, dyn->d_un.d_ptr); break;
      case DT_RELSZ: relsz = (size_t)dyn->d_un.d_val; break;
      case DT_RELENT: relent = (size_t)dyn->d_un.d_val; break;
      default: break;
    }
  }
  if (d.symtab == NULL || d.strtab == NULL) return 0;
  gn_patch_table(&d, jmprel, pltrelsz, pltrel == DT_RELA ? relaent : relent);
  gn_patch_table(&d, rela, relasz, relaent);
  gn_patch_table(&d, rel, relsz, relent);
  return 0;
}

static pthread_once_t gn_onstack_once = PTHREAD_ONCE_INIT;

static void gn_install(void) { (void)dl_iterate_phdr(gn_patch_object, NULL); }

void gn_install_onstack_hook(void) { (void)pthread_once(&gn_onstack_once, gn_install); }

#else /* not Linux, or an architecture without the relocation types above */
void gn_install_onstack_hook(void) {}
#endif
