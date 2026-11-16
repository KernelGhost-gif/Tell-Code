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
extern char **environ;

namespace {
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
}
int main(int argc,char** argv) {
    if(argc==2 && std::string(argv[1])=="--llvm-version") { std::cout << "LLVM " << LLVM_VERSION_STRING << " (host " << llvm::sys::getDefaultTargetTriple() << ")\n"; return 0; }
    std::string input, output="a.out";
    for(int i=1;i<argc;++i){std::string arg=argv[i];if(arg=="-o"){if(++i==argc){std::cerr<<"tellc: -o requires a filename\n";return 2;}output=argv[i];}else if(!arg.empty()&&arg[0]=='-'){std::cerr<<"tellc: unknown option "<<arg<<'\n';return 2;}else if(input.empty())input=arg;else{std::cerr<<"tellc: expected one input file\n";return 2;}}
    if(input.empty()){std::cerr<<"usage: tellc <file.tell> [-o executable]\n";return 2;}
    std::error_code same_error;
    if (std::filesystem::equivalent(input,output,same_error) && !same_error) { std::cerr << "tellc: output must differ from input\n"; return 2; }
    std::ifstream f(input,std::ios::binary);if(!f){std::cerr<<"cannot open "<<input<<'\n';return 2;}
    std::string source((std::istreambuf_iterator<char>(f)),{});
    try {
        auto program=tell::parse(source);
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
        if(run(TELL_LLC_PATH,{TELL_LLC_PATH,"-filetype=obj",target_arg.c_str(),ir_file.path.c_str(),"-o",object_file.path.c_str()})!=0) {
            std::cerr<<"tellc: LLVM machine code generation failed\n";return 1;
        }
        if(run("clang",{"clang",object_file.path.c_str(),"-o",output.c_str()})!=0) {
            std::cerr<<"tellc: linking failed\n";return 1;
        }
    } catch(const tell::Error& e){std::cerr<<input<<':'<<e.location.line<<':'<<e.location.column<<": "<<e.what()<<'\n';return 1;}catch(const std::exception& e){std::cerr<<"tellc: "<<e.what()<<'\n';return 1;}
}
