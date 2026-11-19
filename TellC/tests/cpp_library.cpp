#include <cstdint>
#include <string>
#include <vector>

extern "C" int64_t cpp_value(int64_t n) {
    std::vector<std::string> words;
    words.emplace_back("library");
    return n + static_cast<int64_t>(words.front().size());
}
extern "C" int32_t cpp_twice(int32_t n) { return n * 2; }
extern "C" float cpp_half(float n) { return n / 2; }
extern "C" double cpp_add(double n) { return n + 1.25; }
extern "C" void cpp_noop() {}
