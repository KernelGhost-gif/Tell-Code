#pragma once
#include "tell/parser.hpp"
#ifdef TELL_EMBEDDED_CODEGEN
#include <llvm/ADT/SmallVector.h>
#endif

namespace tell {
#ifdef TELL_EMBEDDED_CODEGEN
void generate_object(const Program& program, llvm::SmallVectorImpl<char>& object, const std::string& target_triple);
#endif
void check_program(const Program& program);
void generate_ir(const Program& program, const std::string& path);
}
