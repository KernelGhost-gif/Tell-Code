#pragma once
#include "tell/parser.hpp"
#include <memory>
#ifdef TELL_EMBEDDED_CODEGEN
#include <llvm/ADT/SmallVector.h>
#endif

namespace llvm {
class LLVMContext;
class Module;
}

namespace tell {
std::unique_ptr<llvm::Module> generate_module(const Program& program, llvm::LLVMContext& context);
#ifdef TELL_EMBEDDED_CODEGEN
void generate_object(const Program& program, llvm::SmallVectorImpl<char>& object, const std::string& target_triple);
#endif
void check_program(const Program& program);
void generate_ir(const Program& program, const std::string& path);
}
