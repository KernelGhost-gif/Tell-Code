#ifdef TELL_STANDALONE_MACOS
#include <llvm/Object/MachOUniversal.h>
#include <llvm/BinaryFormat/MachO.h>
#endif
#include "tell/parser.hpp"
#include "codegen.hpp"
#ifdef TELL_ENABLE_JIT
#include "jit.hpp"
#endif
#ifdef TELL_STANDALONE_LINUX
#include "linux_linker.hpp"
#endif
#include <llvm/Config/llvm-config.h>
#include <llvm/TargetParser/Host.h>
#include <fstream>
#include <cerrno>
#include <cstring>
#include <filesystem>
#include <iostream>
#include <iterator>
#include <spawn.h>
#include <sys/wait.h>
#include <unistd.h>
#include <vector>
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
#ifndef TELL_EMBEDDED_CODEGEN
struct TempFile {
    std::string path;
    TempFile() { char name[]="/tmp/tellc-XXXXXX"; int fd=mkstemp(name); if(fd<0) throw std::runtime_error("cannot create temporary file"); close(fd); path=name; }
    ~TempFile(){if(!path.empty()) unlink(path.c_str());}
};
int run(const char* executable, const std::vector<const char*>& arguments) {
    auto args=arguments; args.push_back(nullptr); pid_t pid;
    int rc=posix_spawnp(&pid,executable,nullptr,nullptr,const_cast<char* const*>(args.data()),environ);
    if(rc) throw std::runtime_error(std::string("could not start ")+executable+": "+std::strerror(rc));
    int status=0; while(waitpid(pid,&status,0)<0){if(errno!=EINTR)throw std::runtime_error("failed waiting for compiler tool");}
    return WIFEXITED(status) ? WEXITSTATUS(status) : 1;
}
#elif defined(TELL_STANDALONE_MACOS)
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

void link_macos(llvm::StringRef object, const std::string& output,
                const std::vector<std::string>& libraries) {
    constexpr const char* minimum = "11.0";
    void* system = dlopen("/usr/lib/libSystem.B.dylib", RTLD_LAZY | RTLD_LOCAL);
    void* cpp = dlopen("/usr/lib/libc++.1.dylib", RTLD_LAZY | RTLD_LOCAL);
    if (!system || !cpp) throw std::runtime_error("cannot load macOS system libraries");
    std::set<std::string> systemSymbols = {"_printf", "_strcmp", "_malloc", "_abort", "dyld_stub_binder"};
    std::set<std::string> cppSymbols;
    auto generated = llvm::object::createBinary(llvm::MemoryBufferRef(object, "tell-generated.o"));
    if (!generated) throw std::runtime_error("cannot inspect generated object: " + llvm::toString(generated.takeError()));
    scan_binary(**generated, system, cpp, systemSymbols, cppSymbols);
    for (const auto& path : libraries) {
        auto input = llvm::object::createBinary(path);
        if (!input) throw std::runtime_error("cannot read library '" + path + "': " + llvm::toString(input.takeError()));
        scan_binary(*input->getBinary(), system, cpp, systemSymbols, cppSymbols);
    }
    dlclose(cpp);
    dlclose(system);
    const std::string stub = make_stub("/usr/lib/libSystem.B.dylib", systemSymbols);
    const std::string cppStub = make_stub("/usr/lib/libc++.1.dylib", cppSymbols);
    constexpr const char* objectName = "tell-generated.o";
    constexpr const char* stubName = "tell-libSystem.tbd";
    constexpr const char* cppStubName = "tell-libc++.tbd";
    lld::macho::cachedReads[llvm::CachedHashStringRef(objectName)] = llvm::MemoryBufferRef(object, objectName);
    lld::macho::cachedReads[llvm::CachedHashStringRef(stubName)] = llvm::MemoryBufferRef(stub, stubName);
    lld::macho::cachedReads[llvm::CachedHashStringRef(cppStubName)] = llvm::MemoryBufferRef(cppStub, cppStubName);
    // LLD's '-' output is constructed in memory and written to stdout; direct
    // stdout to the requested file so it never creates a temporary output file.
    int saved = dup(STDOUT_FILENO);
    if (saved < 0) throw std::runtime_error("cannot save stdout");
    int fd = open(output.c_str(), O_WRONLY | O_CREAT | O_TRUNC, 0777);
    if (fd < 0) { close(saved); throw std::runtime_error("cannot open output: " + std::string(strerror(errno))); }
    if (dup2(fd, STDOUT_FILENO) < 0) { close(fd); close(saved); throw std::runtime_error("cannot redirect linker output"); }
    close(fd);
    std::vector<const char*> args = {"ld64.lld", "-arch", "arm64", "-platform_version", "macos", minimum, minimum,
                                     "-o", "-", objectName};
    for (const auto& library : libraries) args.push_back(library.c_str());
    args.push_back(stubName);
    if (!cppSymbols.empty()) args.push_back(cppStubName);
    const lld::DriverDef drivers[] = {{lld::Darwin, &lld::macho::link}};
    auto result = lld::lldMain(args, llvm::outs(), llvm::errs(), drivers);
    llvm::outs().flush();
    bool writeError = llvm::outs().has_error();
    llvm::outs().clear_error();
    int restore = dup2(saved, STDOUT_FILENO);
    close(saved);
    if (restore < 0) throw std::runtime_error("cannot restore stdout");
    if (result.retCode != 0 || writeError) {
        unlink(output.c_str());
        throw std::runtime_error(writeError ? "cannot write executable" : "linking failed");
    }
}
#endif
}
int main(int argc,char** argv) {
    if(argc==2 && std::string(argv[1])=="--llvm-version") { std::cout << "LLVM " << LLVM_VERSION_STRING << " (host " << llvm::sys::getDefaultTargetTriple() << ")\n"; return 0; }
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
    std::string input, output = "a.out";
    std::vector<std::string> libraries;
    bool jit = false;
    bool output_requested = false;
#ifdef TELL_STANDALONE_LINUX
    std::string target = TELL_LINUX_ARCH;
#endif
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "--jit") {
            jit = true;
        } else if (arg == "--link" || arg == "-o"
#ifdef TELL_STANDALONE_LINUX
            || arg == "--target"
#endif
        ) {
            if (++i == argc) {
                std::cerr << "tellc: " << arg << " requires an argument\n";
                return 2;
            }
            if (arg == "--link") libraries.emplace_back(argv[i]);
            else if (arg == "-o") {
                output = argv[i];
                output_requested = true;
            }
#ifdef TELL_STANDALONE_LINUX
            else target = argv[i];
#endif
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
        std::cerr << "usage: tellc <file.tell> [-o executable] [--link library]..."
#ifdef TELL_STANDALONE_LINUX
                  << " [--target x86_64|aarch64]"
#endif
                  << '\n';
#ifdef TELL_ENABLE_JIT
        std::cerr << "       tellc --jit <file.tell>\n";
#endif
        return 2;
    }
#ifndef TELL_ENABLE_JIT
    if (jit) {
        std::cerr << "tellc: --jit is unavailable in the static Linux build\n";
        return 2;
    }
#endif
    if (jit && (output_requested || !libraries.empty())) {
        std::cerr << "tellc: --jit cannot be combined with -o or --link\n";
        return 2;
    }
#ifdef TELL_STANDALONE_LINUX
    if (target != "x86_64" && target != "aarch64") {
        std::cerr << "tellc: unsupported Linux target " << target << " (expected x86_64 or aarch64)\n";
        return 2;
    }
#endif
    if (!jit) {
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
    }
    std::ifstream f(input,std::ios::binary);if(!f){std::cerr<<"cannot open "<<input<<'\n';return 2;}
    std::string source((std::istreambuf_iterator<char>(f)),{});
    try {
        auto program=tell::parse(source);
#ifdef TELL_ENABLE_JIT
        if (jit) return tell::run_jit(program);
#endif
#ifdef TELL_STANDALONE_MACOS
        llvm::SmallVector<char, 0> object;
        tell::generate_object(program, object, "arm64-apple-macosx11.0.0");
        link_macos(llvm::StringRef(object.data(), object.size()), output, libraries);
#elif defined(TELL_STANDALONE_LINUX)
        llvm::SmallVector<char, 0> object;
        tell::generate_object(program, object, target + "-unknown-linux-musl");
        tell::link_linux(llvm::StringRef(object.data(), object.size()), target, output, libraries);
#else
        TempFile ir_file, object_file, target_probe;
        tell::generate_ir(program,ir_file.path);
        // Ask the linker driver for its effective target, including the SDK and
        // MACOSX_DEPLOYMENT_TARGET, so llc emits a compatible object file.
        if(run("clang",{"clang","-S","-emit-llvm","-x","c","/dev/null","-o",target_probe.path.c_str()})!=0)
            throw std::runtime_error("could not determine Clang target");
        std::ifstream probe(target_probe.path); std::string line, triple;
        while(std::getline(probe,line)) {
            const std::string prefix="target triple = \"";
            if(line.rfind(prefix,0)==0) { auto end=line.find('"',prefix.size()); if(end!=std::string::npos) triple=line.substr(prefix.size(),end-prefix.size()); break; }
        }
        if(triple.empty()) throw std::runtime_error("could not read Clang target triple");
        const std::string target_arg="-mtriple="+triple;
        if (run(TELL_LLC_PATH, {TELL_LLC_PATH, "-filetype=obj", "-relocation-model=pic",
                               target_arg.c_str(), ir_file.path.c_str(), "-o",
                               object_file.path.c_str()}) != 0) {
            std::cerr<<"tellc: LLVM machine code generation failed\n";return 1;
        }
        std::vector<const char*> link_args = {"clang", object_file.path.c_str()};
        for (const auto& library : libraries) link_args.push_back(library.c_str());
#ifdef __APPLE__
        if (!libraries.empty()) link_args.push_back("-lc++");
#else
        if (!libraries.empty()) link_args.push_back("-lstdc++");
#endif
        link_args.push_back("-o"); link_args.push_back(output.c_str());
        if(run("clang",link_args)!=0) {
            std::cerr<<"tellc: linking failed\n";return 1;
        }
#endif
    } catch(const tell::Error& e){std::cerr<<input<<':'<<e.location.line<<':'<<e.location.column<<": "<<e.what()<<'\n';return 1;}catch(const std::exception& e){std::cerr<<"tellc: "<<e.what()<<'\n';return 1;}
}
