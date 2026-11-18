#pragma once

#include <llvm/ADT/StringRef.h>
#include <string>
#include <vector>

namespace tell {
void link_linux(llvm::StringRef object, const std::string& target,
                const std::string& output, const std::vector<std::string>& libraries);
}
