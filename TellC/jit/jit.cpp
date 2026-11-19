#include "jit.hpp"
#include "codegen.hpp"

#include <llvm/ExecutionEngine/Orc/LLJIT.h>
#include <llvm/Support/Error.h>
#include <llvm/Support/TargetSelect.h>

#include <memory>
#include <stdexcept>
#include <utility>

namespace tell {
int run_jit(const Program& program) {
    if (llvm::InitializeNativeTarget() || llvm::InitializeNativeTargetAsmPrinter())
        throw std::runtime_error("cannot initialize LLVM JIT for the host target");

    auto jit = llvm::orc::LLJITBuilder().create();
    if (!jit)
        throw std::runtime_error("cannot create LLVM JIT: " + llvm::toString(jit.takeError()));

    auto context = std::make_unique<llvm::LLVMContext>();
    auto module = generate_module(program, *context);
    module->setTargetTriple((*jit)->getTargetTriple());
    module->setDataLayout((*jit)->getDataLayout());
    if (auto error = (*jit)->addIRModule({std::move(module), std::move(context)}))
        throw std::runtime_error("cannot add Tell IR to LLVM JIT: " + llvm::toString(std::move(error)));

    auto entry = (*jit)->lookup("main");
    if (!entry)
        throw std::runtime_error("cannot resolve Tell entry point: " + llvm::toString(entry.takeError()));
    return entry->toPtr<int()>()();
}
} // namespace tell
