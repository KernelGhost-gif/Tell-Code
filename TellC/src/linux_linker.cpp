#include "linux_linker.hpp"

#include <lld/Common/Driver.h>
#include <llvm/Support/raw_ostream.h>
#include <cerrno>
#include <filesystem>
#include <iterator>
#include <memory>
#include <stdexcept>
#include <system_error>
#include <sys/mman.h>
#include <unistd.h>

LLD_HAS_DRIVER(elf)

namespace {
struct EmbeddedFile {
    const char* name;
    const char* bytes;
    size_t size;
    bool for_foreign_libraries;
};
#include "linux_runtime.hpp"

// ELF LLD takes paths. Anonymous files let it read the embedded inputs
// without extracting a toolchain to disk or depending on TMPDIR.
class LinkerInput {
    int fd;
public:
    LinkerInput(const char* name, llvm::StringRef data)
        : fd(memfd_create(name, MFD_CLOEXEC)) {
        if (fd < 0)
            throw std::system_error(errno, std::generic_category(), "creating linker input");
        while (!data.empty()) {
            auto count = write(fd, data.data(), data.size());
            if (count < 0 && errno == EINTR) continue;
            if (count <= 0) {
                int error = count < 0 ? errno : EIO;
                close(fd);
                throw std::system_error(error, std::generic_category(), "writing linker input");
            }
            data = data.drop_front(count);
        }
    }

    ~LinkerInput() { close(fd); }
    LinkerInput(const LinkerInput&) = delete;
    LinkerInput& operator=(const LinkerInput&) = delete;

    std::string path() const { return "/proc/self/fd/" + std::to_string(fd); }
};
}

void tell::link_linux(llvm::StringRef object, const std::string& target,
                      const std::string& output, const std::vector<std::string>& libraries) {
    bool x86 = target == "x86_64";
    const auto& runtime = x86 ? x86_64_runtime : aarch64_runtime;
    std::vector<std::string> args = {
        "ld.lld", "-m", x86 ? "elf_x86_64" : "aarch64linux",
        "-static", "--gc-sections", "--eh-frame-hdr", "--strip-debug", "-o", output
    };
    LinkerInput generated("tell.o", object);
    std::vector<std::unique_ptr<LinkerInput>> inputs;
    auto add_runtime_file = [&](const EmbeddedFile& file) {
        inputs.push_back(std::make_unique<LinkerInput>(file.name, llvm::StringRef(file.bytes, file.size)));
        args.push_back(inputs.back()->path());
    };

    add_runtime_file(runtime[0]);
    args.push_back(generated.path());
    for (const auto& library : libraries)
        args.push_back(std::filesystem::absolute(library).string());
    // LLD resolves archive references across its inputs and rescans as needed.
    for (size_t i = 1; i < std::size(runtime); ++i) {
        const auto& file = runtime[i];
        if (libraries.empty() && file.for_foreign_libraries) continue;
        add_runtime_file(file);
    }

    std::vector<const char*> argv;
    for (const auto& arg : args) argv.push_back(arg.c_str());
    const lld::DriverDef drivers[] = {{lld::Gnu, &lld::elf::link}};
    if (lld::lldMain(argv, llvm::outs(), llvm::errs(), drivers).retCode)
        throw std::runtime_error("linking failed");
}
