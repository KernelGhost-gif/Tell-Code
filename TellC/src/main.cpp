#include "tell/parser.hpp"
#include "codegen.hpp"

#include <llvm/Config/llvm-config.h>
#include <llvm/TargetParser/Host.h>

#include <cerrno>
#include <cstdio>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <string>
#include <vector>

#include <spawn.h>
#include <sys/wait.h>
#include <unistd.h>

#ifdef TELL_STANDALONE_MACOS
#include <fcntl.h>
#include <llvm/ADT/CachedHashString.h>
#include <llvm/ADT/DenseMap.h>
#include <llvm/Support/MemoryBuffer.h>
#include <llvm/Support/raw_ostream.h>
#include <lld/Common/Driver.h>

LLD_HAS_DRIVER(macho)
namespace lld::macho {
extern llvm::DenseMap<llvm::CachedHashStringRef, llvm::MemoryBufferRef> cachedReads;
}
#endif

extern char **environ;

namespace {

void print_usage(std::ostream& out) {
    out << "TellC - experimental strongly typed Tell compiler\n"
           "Usage:\n"
           "  tellc <file.tell> [-o executable]\n"
           "  tellc --help\n"
           "  tellc --version\n"
           "  tellc --llvm-version\n";
}

#ifndef TELL_STANDALONE_MACOS
struct TempFile {
    std::string path;

    TempFile() {
        std::error_code error;
        const auto directory = std::filesystem::temp_directory_path(error);
        if (error) {
            throw std::runtime_error("cannot locate temporary directory: " + error.message());
        }

        auto pattern = (directory / "tellc-XXXXXX").string();
        std::vector<char> name(pattern.begin(), pattern.end());
        name.push_back('\0');

        const int fd = mkstemp(name.data());
        if (fd < 0) {
            throw std::runtime_error("cannot create temporary file: " + std::string(std::strerror(errno)));
        }

        path = name.data();
        if (close(fd) != 0) {
            const int close_error = errno;
            unlink(path.c_str());
            path.clear();
            throw std::runtime_error("cannot close temporary file: " + std::string(std::strerror(close_error)));
        }
    }

    ~TempFile() {
        if (!path.empty()) {
            unlink(path.c_str());
        }
    }

    TempFile(const TempFile&) = delete;
    TempFile& operator=(const TempFile&) = delete;
};

int run(const char* executable, const std::vector<const char*>& arguments) {
    auto args = arguments;
    args.push_back(nullptr);

    pid_t pid = 0;
    const int spawn_result = posix_spawnp(
        &pid, executable, nullptr, nullptr,
        const_cast<char* const*>(args.data()), environ);
    if (spawn_result != 0) {
        throw std::runtime_error(
            std::string("could not start ") + executable + ": " + std::strerror(spawn_result));
    }

    int status = 0;
    while (waitpid(pid, &status, 0) < 0) {
        if (errno != EINTR) {
            throw std::runtime_error("failed waiting for compiler tool: " + std::string(std::strerror(errno)));
        }
    }

    if (WIFEXITED(status)) {
        return WEXITSTATUS(status);
    }
    if (WIFSIGNALED(status)) {
        return 128 + WTERMSIG(status);
    }
    return 1;
}
#else
class StdoutRedirect {
    int saved_stdout_ = -1;

public:
    explicit StdoutRedirect(const std::string& output, bool& output_created) {
        std::cout.flush();
        if (std::fflush(stdout) != 0) {
            throw std::runtime_error("cannot flush stdout before linking");
        }
        llvm::outs().flush();

        saved_stdout_ = dup(STDOUT_FILENO);
        if (saved_stdout_ < 0) {
            throw std::runtime_error("cannot save stdout: " + std::string(std::strerror(errno)));
        }

        const int output_fd = open(output.c_str(), O_WRONLY | O_CREAT | O_TRUNC, 0755);
        if (output_fd < 0) {
            const int open_error = errno;
            close(saved_stdout_);
            saved_stdout_ = -1;
            throw std::runtime_error("cannot open output '" + output + "': " + std::strerror(open_error));
        }
        output_created = true;

        if (dup2(output_fd, STDOUT_FILENO) < 0) {
            const int redirect_error = errno;
            close(output_fd);
            close(saved_stdout_);
            saved_stdout_ = -1;
            throw std::runtime_error("cannot redirect linker output: " + std::string(std::strerror(redirect_error)));
        }
        close(output_fd);
        llvm::outs().clear_error();
    }

    StdoutRedirect(const StdoutRedirect&) = delete;
    StdoutRedirect& operator=(const StdoutRedirect&) = delete;

    bool restore() {
        const bool stdio_error = std::fflush(stdout) != 0;
        llvm::outs().flush();
        const bool llvm_error = llvm::outs().has_error();
        llvm::outs().clear_error();

        if (saved_stdout_ >= 0) {
            if (dup2(saved_stdout_, STDOUT_FILENO) < 0) {
                throw std::runtime_error("cannot restore stdout: " + std::string(std::strerror(errno)));
            }
            close(saved_stdout_);
            saved_stdout_ = -1;
        }
        return !stdio_error && !llvm_error;
    }

    ~StdoutRedirect() {
        if (saved_stdout_ >= 0) {
            std::fflush(stdout);
            llvm::outs().flush();
            (void)dup2(saved_stdout_, STDOUT_FILENO);
            close(saved_stdout_);
        }
    }
};

void link_macos(llvm::StringRef object, const std::string& output) {
    constexpr const char* arch = "arm64";
    constexpr const char* minimum = "11.0";
    constexpr const char* target = "arm64-macos";
    constexpr const char* object_name = "tell-generated.o";
    constexpr const char* stub_name = "tell-libSystem.tbd";

    // TellC's current backend only imports these stable libSystem C ABI symbols.
    const std::string stub =
        std::string("--- !tapi-tbd\ntbd-version: 4\ntargets: [ ") + target +
        " ]\ninstall-name: /usr/lib/libSystem.B.dylib\nexports:\n"
        "  - targets: [ " + target +
        " ]\n    symbols: [ _printf, _strcmp, _malloc, _abort, dyld_stub_binder ]\n...\n";

    const llvm::CachedHashStringRef object_key(object_name);
    const llvm::CachedHashStringRef stub_key(stub_name);
    lld::macho::cachedReads[object_key] =
        llvm::MemoryBufferRef(object, object_name);
    lld::macho::cachedReads[stub_key] =
        llvm::MemoryBufferRef(stub, stub_name);

    bool output_created = false;
    try {
        StdoutRedirect redirect(output, output_created);
        const char* args[] = {
            "ld64.lld", "-arch", arch, "-platform_version", "macos",
            minimum, minimum, "-o", "-", object_name, stub_name
        };
        const lld::DriverDef drivers[] = {{lld::Darwin, &lld::macho::link}};
        const auto result = lld::lldMain(args, llvm::outs(), llvm::errs(), drivers);
        const bool output_ok = redirect.restore();

        if (result.retCode != 0 || !output_ok) {
            throw std::runtime_error(
                output_ok ? "linking failed" : "cannot write linked executable");
        }
    } catch (...) {
        lld::macho::cachedReads.erase(object_key);
        lld::macho::cachedReads.erase(stub_key);
        if (output_created) {
            unlink(output.c_str());
        }
        throw;
    }

    // Cached MemoryBufferRefs point into stack-owned input buffers. Do not
    // leave those references in LLD's process-global cache after the link.
    lld::macho::cachedReads.erase(object_key);
    lld::macho::cachedReads.erase(stub_key);
}
#endif

} // namespace

int main(int argc, char** argv) {
    // The language server sends unsaved buffers on stdin. This path performs
    // compiler checks without writing output, linking, or executing code.
    if (argc == 2 && std::string(argv[1]) == "--check-stdin") {
        std::string source((std::istreambuf_iterator<char>(std::cin)), {});
        try {
            auto program = tell::parse(source);
            tell::check_program(program);
            return 0;
        } catch (const tell::Error& e) {
            std::cerr << "<stdin>:" << e.location.line << ':' << e.location.column << ": " << e.what() << '\n';
            return 1;
        } catch (const std::exception& e) {
            std::cerr << "tellc: " << e.what() << '\n';
            return 2;
        }
    }
    if (argc == 2) {
        const std::string option = argv[1];
        if (option == "--help" || option == "-h") {
            print_usage(std::cout);
            return 0;
        }
        if (option == "--version") {
            std::cout << "TellC (experimental), LLVM " << LLVM_VERSION_STRING << '\n';
            return 0;
        }
        if (option == "--llvm-version") {
            std::cout << "LLVM " << LLVM_VERSION_STRING
                      << " (host " << llvm::sys::getDefaultTargetTriple() << ")\n";
            return 0;
        }
    }

    std::string input;
    std::string output = "a.out";
    bool output_option_seen = false;

    for (int i = 1; i < argc; ++i) {
        const std::string arg = argv[i];
        if (arg == "-o") {
            if (output_option_seen) {
                std::cerr << "tellc: -o may only be specified once\n";
                return 2;
            }
            if (++i == argc || std::string(argv[i]).empty()) {
                std::cerr << "tellc: -o requires a non-empty filename\n";
                return 2;
            }
            output_option_seen = true;
            output = argv[i];
        } else if (!arg.empty() && arg[0] == '-') {
            std::cerr << "tellc: unknown option " << arg << '\n';
            return 2;
        } else if (input.empty()) {
            input = arg;
        } else {
            std::cerr << "tellc: expected one input file\n";
            return 2;
        }
    }

    if (input.empty()) {
        print_usage(std::cerr);
        return 2;
    }

    std::error_code same_error;
    if (std::filesystem::equivalent(input, output, same_error) && !same_error) {
        std::cerr << "tellc: output must differ from input\n";
        return 2;
    }

    std::ifstream file(input, std::ios::binary);
    if (!file) {
        std::cerr << "tellc: cannot open " << input << '\n';
        return 2;
    }

    const std::string source((std::istreambuf_iterator<char>(file)), {});
    if (file.bad()) {
        std::cerr << "tellc: failed while reading " << input << '\n';
        return 2;
    }

    try {
        const auto program = tell::parse(source);
#ifdef TELL_STANDALONE_MACOS
        llvm::SmallVector<char, 0> object;
        tell::generate_object(program, object);
        link_macos(llvm::StringRef(object.data(), object.size()), output);
#else
        TempFile ir_file;
        TempFile object_file;
        TempFile target_probe;

        tell::generate_ir(program, ir_file.path);

        // Ask Clang for its effective target, including the active SDK and
        // MACOSX_DEPLOYMENT_TARGET, before asking llc to emit the object file.
        if (run("clang", {"clang", "-S", "-emit-llvm", "-x", "c",
                          "/dev/null", "-o", target_probe.path.c_str()}) != 0) {
            throw std::runtime_error("could not determine Clang target");
        }

        std::ifstream probe(target_probe.path);
        std::string line;
        std::string triple;
        while (std::getline(probe, line)) {
            const std::string prefix = "target triple = \"";
            if (line.rfind(prefix, 0) == 0) {
                const auto end = line.find('"', prefix.size());
                if (end != std::string::npos) {
                    triple = line.substr(prefix.size(), end - prefix.size());
                }
                break;
            }
        }
        if (triple.empty()) {
            throw std::runtime_error("could not read Clang target triple");
        }

        const std::string target_arg = "-mtriple=" + triple;
        if (run(TELL_LLC_PATH, {
                TELL_LLC_PATH, "-filetype=obj", "-relocation-model=pic",
                target_arg.c_str(), ir_file.path.c_str(), "-o", object_file.path.c_str()
            }) != 0) {
            throw std::runtime_error("LLVM machine code generation failed");
        }

        if (run("clang", {"clang", object_file.path.c_str(), "-o", output.c_str()}) != 0) {
            throw std::runtime_error("linking failed");
        }
#endif
    } catch (const tell::Error& error) {
        std::cerr << input << ':' << error.location.line << ':' << error.location.column
                  << ": " << error.what() << '\n';
        return 1;
    } catch (const std::exception& error) {
        std::cerr << "tellc: " << error.what() << '\n';
        return 1;
    }

    return 0;
}
