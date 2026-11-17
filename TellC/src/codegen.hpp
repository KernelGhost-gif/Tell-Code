#pragma once
#include "tell/parser.hpp"
#ifdef TELL_STANDALONE_MACOS
#include <llvm/ADT/SmallVector.h>
#endif

namespace tell {
#ifdef TELL_STANDALONE_MACOS
void generate_object(const Program& program, llvm::SmallVectorImpl<char>& object);
#endif
void generate_ir(const Program& program, const std::string& path);
}
