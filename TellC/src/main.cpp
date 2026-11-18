#include "tell/parser.hpp"
#include "codegen.hpp"
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
#ifndef TELL_STANDALONE_MACOS
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
#else
void link_macos(llvm::StringRef object, const std::string& output) {
    constexpr const char* arch = "arm64";
    constexpr const char* minimum = "11.0";
    constexpr const char* target = "arm64-macos";
    // The generated code only uses these stable libSystem C ABI symbols.
    const std::string stub = std::string("--- !tapi-tbd\ntbd-version: 4\ntargets: [ ") + target +
        " ]\ninstall-name: /usr/lib/libSystem.B.dylib\nexports:\n  - targets: [ " + target +
        " ]\n    symbols: [ _printf, _strcmp, _malloc, _abort, dyld_stub_binder ]\n...\n";
    constexpr const char* objectName = "tell-generated.o";
    constexpr const char* stubName = "tell-libSystem.tbd";
    lld::macho::cachedReads[llvm::CachedHashStringRef(objectName)] = llvm::MemoryBufferRef(object, objectName);
    lld::macho::cachedReads[llvm::CachedHashStringRef(stubName)] = llvm::MemoryBufferRef(stub, stubName);
    // LLD's '-' output is constructed in memory and written to stdout; direct
    // stdout to the requested file so it never creates a temporary output file.
    int saved = dup(STDOUT_FILENO);
    if (saved < 0) throw std::runtime_error("cannot save stdout");
    int fd = open(output.c_str(), O_WRONLY | O_CREAT | O_TRUNC, 0777);
    if (fd < 0) { close(saved); throw std::runtime_error("cannot open output: " + std::string(strerror(errno))); }
    if (dup2(fd, STDOUT_FILENO) < 0) { close(fd); close(saved); throw std::runtime_error("cannot redirect linker output"); }
    close(fd);
    const char* args[] = {"ld64.lld", "-arch", arch, "-platform_version", "macos", minimum, minimum,
                          "-o", "-", objectName, stubName};
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
    std::string input, output="a.out";
    for(int i=1;i<argc;++i){std::string arg=argv[i];if(arg=="-o"){if(++i==argc){std::cerr<<"tellc: -o requires a filename\n";return 2;}output=argv[i];}else if(!arg.empty()&&arg[0]=='-'){std::cerr<<"tellc: unknown option "<<arg<<'\n';return 2;}else if(input.empty())input=arg;else{std::cerr<<"tellc: expected one input file\n";return 2;}}
    if(input.empty()){std::cerr<<"usage: tellc <file.tell> [-o executable]\n";return 2;}
    std::error_code same_error;
    if (std::filesystem::equivalent(input,output,same_error) && !same_error) { std::cerr << "tellc: output must differ from input\n"; return 2; }
    std::ifstream f(input,std::ios::binary);if(!f){std::cerr<<"cannot open "<<input<<'\n';return 2;}
    std::string source((std::istreambuf_iterator<char>(f)),{});
    try {
        auto program=tell::parse(source);
#ifdef TELL_STANDALONE_MACOS
        llvm::SmallVector<char, 0> object;
        tell::generate_object(program, object);
        link_macos(llvm::StringRef(object.data(), object.size()), output);
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
        if(run("clang",{"clang",object_file.path.c_str(),"-o",output.c_str()})!=0) {
            std::cerr<<"tellc: linking failed\n";return 1;
        }
#endif
    } catch(const tell::Error& e){std::cerr<<input<<':'<<e.location.line<<':'<<e.location.column<<": "<<e.what()<<'\n';return 1;}catch(const std::exception& e){std::cerr<<"tellc: "<<e.what()<<'\n';return 1;}
}
