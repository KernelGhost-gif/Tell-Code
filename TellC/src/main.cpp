#ifdef TELL_STANDALONE_MACOS
#include <llvm/Object/MachOUniversal.h>
#include <llvm/BinaryFormat/MachO.h>
#endif
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
#include <dlfcn.h>
#include <set>
#include <llvm/Object/Archive.h>
#include <llvm/Object/Binary.h>
#include <llvm/Object/ObjectFile.h>
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
           "  tellc <file.tell> [-o executable] [--link library]...\n"
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
// Build small import stubs for system symbols actually referenced by inputs.
// dlsym checks the running macOS system instead of depending on an installed SDK.
void collect_system_symbols(llvm::object::ObjectFile& object, void* system, void* cpp,
                            std::set<std::string>& systemSymbols, std::set<std::string>& cppSymbols) {
    for (const auto& symbol : object.symbols()) {
        auto flags = symbol.getFlags();
        if (!flags) { llvm::consumeError(flags.takeError()); continue; }
        if (!(*flags & llvm::object::SymbolRef::SF_Undefined)) continue;
        auto name = symbol.getName();
        if (!name) { llvm::consumeError(name.takeError()); continue; }
        if (!name->starts_with("_") || name->size() < 2) continue;
        std::string raw = name->drop_front().str();
        if (dlsym(system, raw.c_str())) systemSymbols.insert(name->str());
        else if (dlsym(cpp, raw.c_str())) cppSymbols.insert(name->str());
    }
}

void scan_binary(llvm::object::Binary& binary, void* system, void* cpp,
                 std::set<std::string>& systemSymbols, std::set<std::string>& cppSymbols) {
    if (auto* object = llvm::dyn_cast<llvm::object::ObjectFile>(&binary)) {
        collect_system_symbols(*object, system, cpp, systemSymbols, cppSymbols);
    } else if (auto* universal = llvm::dyn_cast<llvm::object::MachOUniversalBinary>(&binary)) {
        for (const auto& slice : universal->objects()) {
            if (slice.getArchFlagName() != "arm64") continue;
            auto object = slice.getAsObjectFile();
            if (object) {
                scan_binary(**object, system, cpp, systemSymbols, cppSymbols);
            } else {
                llvm::consumeError(object.takeError());
                auto archive = slice.getAsArchive();
                if (!archive) throw std::runtime_error("cannot inspect ARM64 library slice: " + llvm::toString(archive.takeError()));
                scan_binary(**archive, system, cpp, systemSymbols, cppSymbols);
            }
            return;
        }
        throw std::runtime_error("library does not contain an ARM64 slice");
    } else if (auto* archive = llvm::dyn_cast<llvm::object::Archive>(&binary)) {
        llvm::Error error = llvm::Error::success();
        for (const auto& child : archive->children(error)) {
            auto member = child.getAsBinary();
            if (!member) { llvm::consumeError(member.takeError()); continue; }
            if (auto* object = llvm::dyn_cast<llvm::object::ObjectFile>(member->get()))
                collect_system_symbols(*object, system, cpp, systemSymbols, cppSymbols);
        }
        if (error) throw std::runtime_error("cannot read archive: " + llvm::toString(std::move(error)));
    }
}

std::string make_stub(const char* library, const std::set<std::string>& symbols) {
    std::string stub = std::string("--- !tapi-tbd\ntbd-version: 4\ntargets: [ arm64-macos ]\ninstall-name: ") +
        library + "\nexports:\n  - targets: [ arm64-macos ]\n    symbols: [ ";
    bool first = true;
    for (const auto& name : symbols) {
        if (!first) stub += ", ";
        first = false;
        // Mach-O symbols can contain characters with special YAML meanings.
        stub += "\"";
        for (char c : name) { if (c == '\\' || c == '"') stub += '\\'; stub += c; }
        stub += "\"";
    }
    return stub + " ]\n...\n";
}

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

void link_macos(llvm::StringRef object, const std::string& output, const std::vector<std::string>& libraries) {
    constexpr const char* arch = "arm64";
    constexpr const char* minimum = "11.0";
    constexpr const char* target = "arm64-macos";
    constexpr const char* object_name = "tell-generated.o";
    constexpr const char* stub_name = "tell-libSystem.tbd";

    void* system = dlopen("/usr/lib/libSystem.B.dylib", RTLD_LAZY | RTLD_LOCAL);
    void* cpp = dlopen("/usr/lib/libc++.1.dylib", RTLD_LAZY | RTLD_LOCAL);
    if (!system || !cpp) throw std::runtime_error("cannot load macOS system libraries");
    std::set<std::string> systemSymbols = {"_printf", "_strcmp", "_malloc", "_abort", "dyld_stub_binder"};
    std::set<std::string> cppSymbols;
    auto generated = llvm::object::createBinary(llvm::MemoryBufferRef(object, object_name));
    if (!generated) throw std::runtime_error("cannot inspect generated object: " + llvm::toString(generated.takeError()));
    scan_binary(**generated, system, cpp, systemSymbols, cppSymbols);
    for (const auto& path : libraries) {
        auto binary = llvm::object::createBinary(path);
        if (!binary) throw std::runtime_error("cannot read library '" + path + "': " + llvm::toString(binary.takeError()));
        scan_binary(*binary->getBinary(), system, cpp, systemSymbols, cppSymbols);
    }
    dlclose(cpp); dlclose(system);
    const std::string stub = make_stub("/usr/lib/libSystem.B.dylib", systemSymbols);
    const std::string cpp_stub = make_stub("/usr/lib/libc++.1.dylib", cppSymbols);
    constexpr const char* cpp_stub_name = "tell-libc++.tbd";
    const llvm::CachedHashStringRef cpp_stub_key(cpp_stub_name);
    lld::macho::cachedReads[cpp_stub_key] = llvm::MemoryBufferRef(cpp_stub, cpp_stub_name);

    const llvm::CachedHashStringRef object_key(object_name);
    const llvm::CachedHashStringRef stub_key(stub_name);
    lld::macho::cachedReads[object_key] =
        llvm::MemoryBufferRef(object, object_name);
    lld::macho::cachedReads[stub_key] =
        llvm::MemoryBufferRef(stub, stub_name);

    bool output_created = false;
    try {
        StdoutRedirect redirect(output, output_created);
        std::vector<const char*> args = {
            "ld64.lld", "-arch", arch, "-platform_version", "macos",
            minimum, minimum, "-o", "-", object_name
        };
        for (const auto& library : libraries) args.push_back(library.c_str());
        args.push_back(stub_name);
        if (!cppSymbols.empty()) args.push_back(cpp_stub_name);
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
        lld::macho::cachedReads.erase(cpp_stub_key);
        if (output_created) {
            unlink(output.c_str());
        }
        throw;
    }

    // Cached MemoryBufferRefs point into stack-owned input buffers. Do not
    // leave those references in LLD's process-global cache after the link.
    lld::macho::cachedReads.erase(object_key);
    lld::macho::cachedReads.erase(stub_key);
    lld::macho::cachedReads.erase(cpp_stub_key);
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
    std::vector<std::string> libraries;

    for (int i = 1; i < argc; ++i) {
        const std::string arg = argv[i];
        if (arg == "--link") {
            if (++i == argc || std::string(argv[i]).empty()) { std::cerr << "tellc: --link requires a library filename\n"; return 2; }
            libraries.emplace_back(argv[i]);
        } else if (arg == "-o") {
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

    for (const auto& library : libraries) {
        std::error_code ec;
        if (std::filesystem::equivalent(library, output, ec) && !ec) {
            std::cerr << "tellc: output must differ from library " << library << '\n'; return 2;
        }
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
        link_macos(llvm::StringRef(object.data(), object.size()), output, libraries);
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

        std::vector<const char*> link_args = {"clang", object_file.path.c_str()};
        for (const auto& library : libraries) link_args.push_back(library.c_str());
#ifdef __APPLE__
        if (!libraries.empty()) link_args.push_back("-lc++");
#else
        if (!libraries.empty()) link_args.push_back("-lstdc++");
#endif
        link_args.push_back("-o"); link_args.push_back(output.c_str());
        if (run("clang", link_args) != 0) {
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
