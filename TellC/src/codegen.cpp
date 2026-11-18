#include "codegen.hpp"
#include <llvm/Config/llvm-config.h>
#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/Module.h>
#include <llvm/IR/Verifier.h>
#include <llvm/Support/raw_ostream.h>
#ifdef TELL_EMBEDDED_CODEGEN
#include <llvm/MC/TargetRegistry.h>
#include <llvm/Target/TargetMachine.h>
#include <llvm/TargetParser/Host.h>
#include <llvm/Support/TargetSelect.h>
#include <llvm/IR/LegacyPassManager.h>
#endif
#include <algorithm>
#include <cctype>
#include <map>
#include <set>

namespace tell {
namespace {
struct Value { llvm::Value* value; std::string type; bool slot = false; bool constant = false; };
struct Routine {
    const Stmt* syntax;
    llvm::Function* function;
    bool is_private;
};

struct Field {
    const Stmt* syntax;
    unsigned index;
    bool is_private;
};

struct Object {
    llvm::StructType* layout;
    llvm::Function* constructor = nullptr;
    std::map<std::string, Field> fields;
    std::map<std::string, Routine> methods;
};

class Generator {
    llvm::LLVMContext context;
    llvm::Module module{"tell", context};
#if LLVM_VERSION_MAJOR >= 24
    llvm::IRBuilder<> ir{module};
#else
    llvm::IRBuilder<> ir{context};
#endif
    std::map<std::string, Value> variables;
    llvm::FunctionCallee printf_fn;
    std::map<std::string, Object> objects;
    std::map<std::string, Routine> functions;
    std::map<std::string, const Stmt*> foreign_functions;
    Value receiver{nullptr, ""};
    std::vector<std::pair<llvm::BasicBlock*, llvm::BasicBlock*>> loops;
    bool is_list(const std::string& type) { return type.rfind("list of ", 0) == 0; }
    llvm::StructType* list_layout() { return llvm::StructType::get(context, {ir.getInt64Ty(), ir.getPtrTy()}); }
    llvm::AllocaInst* entry_alloca(llvm::Type* type) {
        auto& entry = ir.GetInsertBlock()->getParent()->getEntryBlock();
#if LLVM_VERSION_MAJOR >= 24
        llvm::IRBuilder<> builder(entry.begin());
#else
        llvm::IRBuilder<> builder(&entry, entry.begin());
#endif
        return builder.CreateAlloca(type);
    }
    llvm::Value* allocate(llvm::Value* size) {
        auto signature = llvm::FunctionType::get(ir.getPtrTy(), {ir.getInt64Ty()}, false);
        auto ptr = ir.CreateCall(module.getOrInsertFunction("malloc", signature), {size});
        auto fn = ir.GetInsertBlock()->getParent();
        auto ok = llvm::BasicBlock::Create(context, "allocated", fn);
        auto bad = llvm::BasicBlock::Create(context, "out_of_memory", fn);
        ir.CreateCondBr(ir.CreateIsNull(ptr), bad, ok);
        ir.SetInsertPoint(bad);
        ir.CreateCall(module.getOrInsertFunction("abort", llvm::FunctionType::get(ir.getVoidTy(), false)));
        ir.CreateUnreachable();
        ir.SetInsertPoint(ok);
        return ptr;
    }
    const Routine* current_routine = nullptr;
    llvm::GlobalVariable* pending_error = nullptr;
    llvm::GlobalVariable* input_stream = nullptr;
    std::vector<llvm::BasicBlock*> handlers;

    void propagate_error() {
        if (!handlers.empty()) { ir.CreateBr(handlers.back()); return; }
        auto fn = ir.GetInsertBlock()->getParent();
        if (fn->getName() == "main") {
            auto message = ir.CreateLoad(ir.getPtrTy(), pending_error);
            auto dprintf_type = llvm::FunctionType::get(ir.getInt32Ty(), {ir.getInt32Ty(), ir.getPtrTy()}, true);
            ir.CreateCall(module.getOrInsertFunction("dprintf", dprintf_type), {ir.getInt32(2), text("Error: %s\n"), message});
            ir.CreateRet(ir.getInt32(1));
        } else if (fn->getReturnType()->isVoidTy()) ir.CreateRetVoid();
        else ir.CreateRet(llvm::Constant::getNullValue(fn->getReturnType()));
    }
    void raise_if(llvm::Value* bad, const std::string& message) {
        auto fn = ir.GetInsertBlock()->getParent();
        auto error = llvm::BasicBlock::Create(context, "error", fn);
        auto ok = llvm::BasicBlock::Create(context, "ok", fn);
        ir.CreateCondBr(bad, error, ok);
        ir.SetInsertPoint(error);
        ir.CreateStore(text(message), pending_error);
        propagate_error();
        ir.SetInsertPoint(ok);
    }
    void check_pending_error() {
        auto fn = ir.GetInsertBlock()->getParent();
        auto error = llvm::BasicBlock::Create(context, "call.error", fn);
        auto ok = llvm::BasicBlock::Create(context, "call.ok", fn);
        ir.CreateCondBr(ir.CreateIsNotNull(ir.CreateLoad(ir.getPtrTy(), pending_error)), error, ok);
        ir.SetInsertPoint(error);
        propagate_error();
        ir.SetInsertPoint(ok);
    }
    Value builtin_input(const Expr& expr) {
        if (expr.children.size() > 2) fail(expr.location, "input takes zero or one argument");
        if (expr.children.size() == 2) {
            auto prompt = expression(*expr.children[1]);
            require_type(prompt, "text", expr.children[1]->location);
            ir.CreateCall(printf_fn, {text("%s"), prompt.value});
        }
        auto fn = ir.GetInsertBlock()->getParent();
        auto stream = ir.CreateLoad(ir.getPtrTy(), input_stream);
        auto open = llvm::BasicBlock::Create(context, "input.open", fn);
        auto ready = llvm::BasicBlock::Create(context, "input.ready", fn);
        ir.CreateCondBr(ir.CreateIsNull(stream), open, ready);
        ir.SetInsertPoint(open);
        auto fdopen_type = llvm::FunctionType::get(ir.getPtrTy(), {ir.getInt32Ty(), ir.getPtrTy()}, false);
        auto created = ir.CreateCall(module.getOrInsertFunction("fdopen", fdopen_type), {ir.getInt32(0), text("r")});
        ir.CreateStore(created, input_stream);
        ir.CreateBr(ready);
        ir.SetInsertPoint(ready);
        stream = ir.CreateLoad(ir.getPtrTy(), input_stream);
        raise_if(ir.CreateIsNull(stream), "cannot open standard input");
        auto buffer = entry_alloca(ir.getPtrTy());
        auto size = entry_alloca(ir.getInt64Ty());
        ir.CreateStore(llvm::ConstantPointerNull::get(ir.getPtrTy()), buffer);
        ir.CreateStore(ir.getInt64(0), size);
        auto fflush_type = llvm::FunctionType::get(ir.getInt32Ty(), {ir.getPtrTy()}, false);
        ir.CreateCall(module.getOrInsertFunction("fflush", fflush_type), {llvm::ConstantPointerNull::get(ir.getPtrTy())});
        auto getline_type = llvm::FunctionType::get(ir.getInt64Ty(), {ir.getPtrTy(), ir.getPtrTy(), ir.getPtrTy()}, false);
        auto length = ir.CreateCall(module.getOrInsertFunction("getline", getline_type), {buffer, size, stream});
        raise_if(ir.CreateICmpSLT(length, ir.getInt64(0)), "cannot read input");
        auto str = ir.CreateLoad(ir.getPtrTy(), buffer);
        auto memchr_type = llvm::FunctionType::get(ir.getPtrTy(), {ir.getPtrTy(), ir.getInt32Ty(), ir.getInt64Ty()}, false);
        auto nul = ir.CreateCall(module.getOrInsertFunction("memchr", memchr_type), {str, ir.getInt32(0), length});
        raise_if(ir.CreateIsNotNull(nul), "input contains NUL bytes");
        auto strip = llvm::BasicBlock::Create(context, "input.strip", fn);
        auto done = llvm::BasicBlock::Create(context, "input.done", fn);
        ir.CreateCondBr(ir.CreateICmpSGT(length, ir.getInt64(0)), strip, done);
        ir.SetInsertPoint(strip);
        auto last = ir.CreateGEP(ir.getInt8Ty(), str, ir.CreateSub(length, ir.getInt64(1)));
        auto newline = ir.CreateICmpEQ(ir.CreateLoad(ir.getInt8Ty(), last), ir.getInt8(10));
        auto remove = llvm::BasicBlock::Create(context, "input.remove", fn);
        ir.CreateCondBr(newline, remove, done);
        ir.SetInsertPoint(remove); ir.CreateStore(ir.getInt8(0), last);
        auto check_cr = llvm::BasicBlock::Create(context, "input.check_cr", fn);
        ir.CreateCondBr(ir.CreateICmpSGT(length, ir.getInt64(1)), check_cr, done);
        ir.SetInsertPoint(check_cr);
        auto previous = ir.CreateGEP(ir.getInt8Ty(), str, ir.CreateSub(length, ir.getInt64(2)));
        auto remove_cr = llvm::BasicBlock::Create(context, "input.remove_cr", fn);
        ir.CreateCondBr(ir.CreateICmpEQ(ir.CreateLoad(ir.getInt8Ty(), previous), ir.getInt8(13)), remove_cr, done);
        ir.SetInsertPoint(remove_cr); ir.CreateStore(ir.getInt8(0), previous); ir.CreateBr(done);
        ir.SetInsertPoint(done);
        return {str, "text"};
    }
    Value builtin_file(const Expr& expr, const std::string& operation) {
        auto argc = operation == "read_file" ? 1u : 2u;
        if (expr.children.size() != argc + 1) fail(expr.location, operation + " has wrong number of arguments");
        auto path = expression(*expr.children[1]);
        require_type(path, "text", expr.children[1]->location);
        Value content{nullptr, ""};
        if (argc == 2) {
            content = expression(*expr.children[2]);
            require_type(content, "text", expr.children[2]->location);
        }
        auto ptr = ir.getPtrTy(); auto i64 = ir.getInt64Ty(); auto i32 = ir.getInt32Ty();
        auto fopen_fn = module.getOrInsertFunction("fopen", llvm::FunctionType::get(ptr, {ptr, ptr}, false));
        auto fclose_fn = module.getOrInsertFunction("fclose", llvm::FunctionType::get(i32, {ptr}, false));
        auto file = ir.CreateCall(fopen_fn, {path.value, text(operation == "read_file" ? "rb" : operation == "write_file" ? "wb" : "ab")});
        raise_if(ir.CreateIsNull(file), "cannot open file");
        auto file_error = [&](llvm::Value* bad, const std::string& message) {
            auto fn = ir.GetInsertBlock()->getParent();
            auto error = llvm::BasicBlock::Create(context, "file.error", fn);
            auto ok = llvm::BasicBlock::Create(context, "file.ok", fn);
            ir.CreateCondBr(bad, error, ok);
            ir.SetInsertPoint(error); ir.CreateCall(fclose_fn, {file});
            ir.CreateStore(text(message), pending_error); propagate_error();
            ir.SetInsertPoint(ok);
        };
        if (argc == 2) {
            auto strlen_fn = module.getOrInsertFunction("strlen", llvm::FunctionType::get(i64, {ptr}, false));
            auto length = ir.CreateCall(strlen_fn, {content.value});
            auto fwrite_fn = module.getOrInsertFunction("fwrite", llvm::FunctionType::get(i64, {ptr, i64, i64, ptr}, false));
            auto written = ir.CreateCall(fwrite_fn, {content.value, ir.getInt64(1), length, file});
            file_error(ir.CreateICmpNE(written, length), "cannot write file");
            auto closed = ir.CreateCall(fclose_fn, {file});
            raise_if(ir.CreateICmpNE(closed, ir.getInt32(0)), "cannot close file");
            return {nullptr, "void"};
        }
        auto seek = module.getOrInsertFunction("fseek", llvm::FunctionType::get(i32, {ptr, i64, i32}, false));
        auto tell = module.getOrInsertFunction("ftell", llvm::FunctionType::get(i64, {ptr}, false));
        auto seek_end = ir.CreateCall(seek, {file, ir.getInt64(0), ir.getInt32(2)});
        file_error(ir.CreateICmpNE(seek_end, ir.getInt32(0)), "cannot read file");
        auto length = ir.CreateCall(tell, {file});
        file_error(ir.CreateOr(ir.CreateICmpSLT(length, ir.getInt64(0)), ir.CreateICmpEQ(length, ir.getInt64(INT64_MAX))), "cannot read file");
        auto seek_start = ir.CreateCall(seek, {file, ir.getInt64(0), ir.getInt32(0)});
        file_error(ir.CreateICmpNE(seek_start, ir.getInt32(0)), "cannot read file");
        auto data = allocate(ir.CreateAdd(length, ir.getInt64(1)));
        auto fread_fn = module.getOrInsertFunction("fread", llvm::FunctionType::get(i64, {ptr, i64, i64, ptr}, false));
        auto bytes = ir.CreateCall(fread_fn, {data, ir.getInt64(1), length, file});
        file_error(ir.CreateICmpNE(bytes, length), "cannot read file");
        ir.CreateStore(ir.getInt8(0), ir.CreateGEP(ir.getInt8Ty(), data, length));
        auto closed = ir.CreateCall(fclose_fn, {file});
        raise_if(ir.CreateICmpNE(closed, ir.getInt32(0)), "cannot close file");
        auto strlen_fn = module.getOrInsertFunction("strlen", llvm::FunctionType::get(i64, {ptr}, false));
        raise_if(ir.CreateICmpNE(ir.CreateCall(strlen_fn, {data}), length), "file contains NUL bytes");
        return {data, "text"};
    }
    Value builtin_number(const Expr& expr, const std::string& type) {
        if (expr.children.size() != 2) fail(expr.location, type + " requires one argument");
        auto value = expression(*expr.children[1]);
        require_type(value, "text", expr.children[1]->location);
        auto end = entry_alloca(ir.getPtrTy());
#if defined(__APPLE__)
        auto errno_fn = "__error";
#else
        auto errno_fn = "__errno_location";
#endif
        auto errno_ptr = ir.CreateCall(module.getOrInsertFunction(errno_fn, llvm::FunctionType::get(ir.getPtrTy(), false)));
        ir.CreateStore(ir.getInt32(0), errno_ptr);
        llvm::Value* result;
        if (type == "number") {
            auto signature = llvm::FunctionType::get(ir.getInt64Ty(), {ir.getPtrTy(), ir.getPtrTy(), ir.getInt32Ty()}, false);
            result = ir.CreateCall(module.getOrInsertFunction("strtoll", signature), {value.value, end, ir.getInt32(10)});
        } else {
            auto signature = llvm::FunctionType::get(ir.getDoubleTy(), {ir.getPtrTy(), ir.getPtrTy()}, false);
            result = ir.CreateCall(module.getOrInsertFunction("strtod", signature), {value.value, end});
        }
        auto endptr = ir.CreateLoad(ir.getPtrTy(), end);
        auto empty = ir.CreateICmpEQ(value.value, endptr);
        auto trailing = ir.CreateICmpNE(ir.CreateLoad(ir.getInt8Ty(), endptr), ir.getInt8(0));
        auto range = ir.CreateICmpNE(ir.CreateLoad(ir.getInt32Ty(), errno_ptr), ir.getInt32(0));
        raise_if(ir.CreateOr(empty, ir.CreateOr(trailing, range)), "invalid " + type);
        return {result, type};
    }


    llvm::Type* llvm_type(const std::string& type, Location loc) {
        if (is_list(type)) { llvm_type(type.substr(8), loc); return ir.getPtrTy(); }
        if (type == "text" || objects.count(type)) return ir.getPtrTy();
        if (type == "number") return ir.getInt64Ty();
        if (type == "decimal") return ir.getDoubleTy();
        if (type == "bool") return ir.getInt1Ty();
        if (type == "void") return ir.getVoidTy();
        fail(loc, "unknown type '" + type + "'");
    }

    void require_type(const Value& value, const std::string& type, Location loc) {
        if (value.type != type)
            fail(loc, "expected '" + type + "', got '" + value.type + "'");
    }

    Value field(Value object, const std::string& name, Location loc) {
        auto owner = objects.find(object.type);
        if (owner == objects.end()) fail(loc, "member access requires an object");
        auto member = owner->second.fields.find(name);
        if (member == owner->second.fields.end()) fail(loc, "unknown field '" + name + "'");
        const auto& info = member->second;
        if (info.is_private && receiver.type != object.type)
            fail(loc, "field '" + name + "' is private");
        auto address = ir.CreateStructGEP(owner->second.layout, object.value, info.index);
        return {ir.CreateLoad(llvm_type(info.syntax->type, loc), address), info.syntax->type};
    }

    Value lookup(const std::string& name, Location loc) {
        auto local = variables.find(name);
        if (local != variables.end()) {
            auto value = local->second;
            if (value.slot) value.value = ir.CreateLoad(llvm_type(value.type, loc), value.value);
            return value;
        }
        if (receiver.value) {
            if (name == "self") return receiver;
            if (objects.at(receiver.type).fields.count(name)) return field(receiver, name, loc);
        }
        fail(loc, "unknown variable '" + name + "'");
    }

    Value call(const Expr& expr) {
        const auto& callee = *expr.children[0];
        const Routine* routine = nullptr;
        std::vector<llvm::Value*> args;
        if (callee.kind == Expr::Kind::Member) {
            auto object = expression(*callee.children[0]);
            auto owner = objects.find(object.type);
            if (owner == objects.end()) fail(callee.location, "method calls require an object");
            auto method = owner->second.methods.find(callee.value);
            if (method == owner->second.methods.end())
                fail(callee.location, "unknown method '" + callee.value + "'");
            routine = &method->second;
            if (routine->is_private && receiver.type != object.type)
                fail(callee.location, "method '" + callee.value + "' is private");
            args.push_back(object.value);
        } else if (callee.kind == Expr::Kind::Name) {
            if (callee.value == "input") return builtin_input(expr);
            if (callee.value == "read_file" || callee.value == "write_file" || callee.value == "append_file") return builtin_file(expr, callee.value);
            if (callee.value == "number" || callee.value == "decimal") return builtin_number(expr, callee.value);
            if (variables.count(callee.value))
                fail(callee.location, "variable '" + callee.value + "' is not callable");
            if (receiver.value) {
                auto& methods = objects.at(receiver.type).methods;
                auto method = methods.find(callee.value);
                if (method != methods.end()) {
                    routine = &method->second;
                    args.push_back(receiver.value);
                }
            }
            if (!routine) {
                auto object = objects.find(callee.value);
                if (object != objects.end()) {
                    if (expr.children.size() != 1) fail(expr.location, "object construction takes no arguments");
                    auto instance = ir.CreateCall(object->second.constructor);
                    check_pending_error();
                    return {instance, callee.value};
                }
                auto foreign = foreign_functions.find(callee.value);
                if (foreign != foreign_functions.end()) return call_foreign(expr, *foreign->second);
                auto function = functions.find(callee.value);
                if (function == functions.end()) fail(callee.location, "unknown function '" + callee.value + "'");
                routine = &function->second;
            }
        } else {
            fail(callee.location, "expected a function or method name");
        }
        const auto& parameters = routine->syntax->parameters;
        if (expr.children.size() - 1 != parameters.size())
            fail(expr.location, "wrong number of arguments to '" + callee.value + "'");
        for (size_t i = 0; i < parameters.size(); ++i) {
            const auto& argument = *expr.children[i + 1];
            auto value = expression(argument, parameters[i].type);
            require_type(value, parameters[i].type, argument.location);
            args.push_back(value.value);
        }
        auto result = ir.CreateCall(routine->function, args);
        check_pending_error();
        return {result, routine->syntax->return_type};
    }

    llvm::Type* foreign_type(const std::string& type) {
        if (type == "int32") return ir.getInt32Ty();
        if (type == "int64") return ir.getInt64Ty();
        if (type == "float32") return ir.getFloatTy();
        if (type == "float64") return ir.getDoubleTy();
        return ir.getVoidTy();
    }

    Value call_foreign(const Expr& expr, const Stmt& declaration) {
        if (expr.children.size() - 1 != declaration.foreign_parameters.size())
            fail(expr.location, "wrong number of arguments to '" + declaration.name + "'");
        std::vector<llvm::Value*> args;
        for (size_t i = 0; i < declaration.foreign_parameters.size(); ++i) {
            const auto& param = declaration.foreign_parameters[i];
            const auto& argument = *expr.children[i + 1];
            auto value = expression(argument);
            bool integer = param.type == "int32" || param.type == "int64";
            require_type(value, integer ? "number" : "decimal", argument.location);
            if (param.type == "int32") {
                auto narrowed = ir.CreateTrunc(value.value, ir.getInt32Ty());
                auto fn = ir.GetInsertBlock()->getParent();
                auto valid = llvm::BasicBlock::Create(context, "foreign.int32.ok", fn);
                auto invalid = llvm::BasicBlock::Create(context, "foreign.int32.overflow", fn);
                ir.CreateCondBr(ir.CreateICmpEQ(value.value, ir.CreateSExt(narrowed, ir.getInt64Ty())), valid, invalid);
                ir.SetInsertPoint(invalid);
                ir.CreateCall(module.getOrInsertFunction("abort", llvm::FunctionType::get(ir.getVoidTy(), false)));
                ir.CreateUnreachable();
                ir.SetInsertPoint(valid);
                value.value = narrowed;
            } else if (param.type == "float32") {
                value.value = ir.CreateFPTrunc(value.value, ir.getFloatTy());
            }
            args.push_back(value.value);
        }
        auto result = ir.CreateCall(module.getFunction(declaration.foreign_symbol), args);
        if (declaration.type == "void") return {result, "void"};
        if (declaration.type == "int32") return {ir.CreateSExt(result, ir.getInt64Ty()), "number"};
        if (declaration.type == "int64") return {result, "number"};
        if (declaration.type == "float32") return {ir.CreateFPExt(result, ir.getDoubleTy()), "decimal"};
        return {result, "decimal"};
    }

    [[noreturn]] void fail(tell::Location loc, const std::string& message) { throw tell::Error(loc, message); }
    llvm::Value* text(const std::string& s) { return ir.CreateGlobalString(s); }
    llvm::Value* equal_values(Value lhs, Value rhs, Location loc) {
        require_type(rhs, lhs.type, loc);
        if (lhs.type == "void") fail(loc, "cannot compare void values");
        if (lhs.type == "text") {
            auto signature = llvm::FunctionType::get(ir.getInt32Ty(), {ir.getPtrTy(), ir.getPtrTy()}, false);
            auto strcmp_fn = module.getOrInsertFunction("strcmp", signature);
            return ir.CreateICmpEQ(ir.CreateCall(strcmp_fn, {lhs.value, rhs.value}), ir.getInt32(0));
        }
        if (lhs.type == "decimal") return ir.CreateFCmpOEQ(lhs.value, rhs.value);
        return ir.CreateICmpEQ(lhs.value, rhs.value);
    }
    Value index_address(const Expr& e) {
        auto list = expression(*e.children[0]);
        if (!is_list(list.type)) fail(e.location, "indexing requires a list or vector");
        auto index = expression(*e.children[1]);
        require_type(index, "number", e.children[1]->location);
        auto layout = list_layout();
        auto length = ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(layout, list.value, 0));
        raise_if(ir.CreateICmpUGE(index.value, length), "index out of bounds");
        auto data = ir.CreateLoad(ir.getPtrTy(), ir.CreateStructGEP(layout, list.value, 1));
        auto type = list.type.substr(8);
        return {ir.CreateGEP(llvm_type(type, e.location), data, index.value), type};
    }
    Value expression(const tell::Expr& e, const std::string& expected = "") {
        using K = tell::Expr::Kind;
        if(e.kind==K::String) {
            if (e.value.find(char(0)) != std::string::npos) fail(e.location, "text literals cannot contain NUL");
            return {text(e.value),"text"};
        }
        if(e.kind==K::Boolean) { auto word=e.value; std::transform(word.begin(),word.end(),word.begin(),[](unsigned char c){return static_cast<char>(std::tolower(c));}); return {ir.getInt1(word=="true"),"bool"}; }
        if(e.kind==K::Number) {
            try {
                if(e.value.find('.')!=std::string::npos) return {llvm::ConstantFP::get(ir.getDoubleTy(),std::stod(e.value)),"decimal"};
                return {llvm::ConstantInt::getSigned(ir.getInt64Ty(),std::stoll(e.value)),"number"};
            } catch(const std::exception&) { fail(e.location,"number out of range"); }
        }
        if (e.kind == K::List) {
            std::string element = is_list(expected) ? expected.substr(8) : "";
            std::vector<Value> values;
            for (const auto& child : e.children) {
                auto value = expression(*child, element);
                if (element.empty()) element = value.type;
                if (element == "void") fail(child->location, "collection element cannot be void");
                require_type(value, element, child->location);
                values.push_back(value);
            }
            if (element.empty()) fail(e.location, "empty list requires an explicit collection type");
            auto element_type = llvm_type(element, e.location);
            auto null = llvm::ConstantPointerNull::get(ir.getPtrTy());
            auto size = ir.CreatePtrToInt(ir.CreateGEP(element_type, null, ir.getInt64(values.empty() ? 1 : values.size())), ir.getInt64Ty());
            auto data = allocate(size);
            for (size_t i = 0; i < values.size(); ++i)
                ir.CreateStore(values[i].value, ir.CreateGEP(element_type, data, ir.getInt64(i)));
            auto layout = list_layout();
            auto header_size = ir.CreatePtrToInt(ir.CreateGEP(layout, null, ir.getInt64(1)), ir.getInt64Ty());
            auto header = allocate(header_size);
            ir.CreateStore(ir.getInt64(values.size()), ir.CreateStructGEP(layout, header, 0));
            ir.CreateStore(data, ir.CreateStructGEP(layout, header, 1));
            return {header, "list of " + element};
        }
        if (e.kind == K::Index) {
            auto address = index_address(e);
            return {ir.CreateLoad(llvm_type(address.type, e.location), address.value), address.type};
        }
        if (e.kind == K::Name) return lookup(e.value, e.location);
        if (e.kind == K::Member) return field(expression(*e.children[0]), e.value, e.location);
        if (e.kind == K::Call) return call(e);
        if (e.kind == K::Binary) {
            auto lhs = expression(*e.children[0]);
            auto rhs = expression(*e.children[1]);
            require_type(rhs, lhs.type, e.children[1]->location);
            if (e.value == "=" || e.value == "==" || e.value == "!=") {
                auto equal = equal_values(lhs, rhs, e.children[1]->location);
                return {e.value == "!=" ? ir.CreateNot(equal) : equal, "bool"};
            }
            if (lhs.type == "number") {
                if (e.value == "+") return {ir.CreateAdd(lhs.value, rhs.value), "number"};
                if (e.value == "-") return {ir.CreateSub(lhs.value, rhs.value), "number"};
                if (e.value == "*") return {ir.CreateMul(lhs.value, rhs.value), "number"};
                if (e.value == "<") return {ir.CreateICmpSLT(lhs.value, rhs.value), "bool"};
                if (e.value == "<=") return {ir.CreateICmpSLE(lhs.value, rhs.value), "bool"};
                if (e.value == ">") return {ir.CreateICmpSGT(lhs.value, rhs.value), "bool"};
                if (e.value == ">=") return {ir.CreateICmpSGE(lhs.value, rhs.value), "bool"};
            }
        }
        fail(e.location,"expression is not yet supported by the executable backend");
    }
    void print_value(Value v, Location loc) {
        if(v.type=="text") ir.CreateCall(printf_fn,{text("%s"),v.value});
        else if(v.type=="number") ir.CreateCall(printf_fn,{text("%lld"),v.value});
        else if(v.type=="decimal") ir.CreateCall(printf_fn,{text("%g"),v.value});
        else if(v.type=="bool") ir.CreateCall(printf_fn,{text("%s"),ir.CreateSelect(v.value,text("true"),text("false"))});
        else fail(loc, "cannot print a value of type '" + v.type + "'");
    }
    void print_string(const tell::Expr& e) {
        std::string part;
        for(size_t i=0;i<e.value.size();) {
            if(e.value[i]!='{') {part+=e.value[i++];continue;}
            size_t end=e.value.find('}',i+1);
            if(end==std::string::npos) fail(e.location,"unclosed interpolation");
            if(!part.empty()){print_value({text(part),"text"}, e.location);part.clear();}
            auto name=e.value.substr(i+1,end-i-1);
            print_value(lookup(name, e.location), e.location);
            i=end+1;
        }
        if(!part.empty()) print_value({text(part),"text"}, e.location);
    }
    Routine declare_routine(const Stmt& stmt, const std::string& owner, bool is_private) {
        if (stmt.return_type.empty() || stmt.parameters.size() != stmt.names.size())
            fail(stmt.location, "executable functions require typed parameters and an 'out' type");
        std::vector<llvm::Type*> parameters;
        if (!owner.empty()) parameters.push_back(ir.getPtrTy());
        std::set<std::string> names;
        for (const auto& parameter : stmt.parameters) {
            if (parameter.name == "self" || !names.insert(parameter.name).second)
                fail(parameter.location, "invalid or duplicate parameter '" + parameter.name + "'");
            parameters.push_back(llvm_type(parameter.type, parameter.location));
        }
        auto signature = llvm::FunctionType::get(llvm_type(stmt.return_type, stmt.location), parameters, false);
        auto name = owner.empty() ? "tell.function." + stmt.name : "tell.method." + owner + "." + stmt.name;
        return {&stmt, llvm::Function::Create(signature, llvm::Function::InternalLinkage, name, module), is_private};
    }

    void declare(const Program& program) {
        for (const auto& stmt : program.statements) {
            if (stmt.kind != Stmt::Kind::Object) continue;
            if (objects.count(stmt.name) || stmt.name == "text" || stmt.name == "number" ||
                stmt.name == "decimal" || stmt.name == "bool" || stmt.name == "void")
                fail(stmt.location, "invalid or duplicate object type '" + stmt.name + "'");
            objects.emplace(stmt.name, Object{llvm::StructType::create(context, stmt.name)});
        }
        for (const auto& stmt : program.statements) {
            if (stmt.kind == Stmt::Kind::Function) {
                if (objects.count(stmt.name) || functions.count(stmt.name) || foreign_functions.count(stmt.name))
                    fail(stmt.location, "duplicate callable '" + stmt.name + "'");
                functions.emplace(stmt.name, declare_routine(stmt, "", false));
            }
            if (stmt.kind == Stmt::Kind::ForeignFunction) {
                if (objects.count(stmt.name) || functions.count(stmt.name) || foreign_functions.count(stmt.name))
                    fail(stmt.location, "duplicate callable '" + stmt.name + "'");
                if (stmt.foreign_symbol == "main" || stmt.foreign_symbol == "malloc" ||
                    stmt.foreign_symbol == "strcmp" || stmt.foreign_symbol == "abort" ||
                    module.getNamedValue(stmt.foreign_symbol))
                    fail(stmt.location, "duplicate or reserved external symbol '" + stmt.foreign_symbol + "'");
                std::vector<llvm::Type*> args;
                std::set<std::string> names;
                for (const auto& param : stmt.foreign_parameters) {
                    if (!names.insert(param.name).second) fail(param.location, "duplicate foreign parameter '" + param.name + "'");
                    args.push_back(foreign_type(param.type));
                }
                auto signature = llvm::FunctionType::get(foreign_type(stmt.type), args, false);
                llvm::Function::Create(signature, llvm::Function::ExternalLinkage, stmt.foreign_symbol, module);
                foreign_functions.emplace(stmt.name, &stmt);
            }
            if (stmt.kind != Stmt::Kind::Object) continue;
            auto& object = objects.at(stmt.name);
            std::vector<llvm::Type*> fields;
            for (const auto& section : stmt.body) {
                for (const auto& member : section.body) {
                    if (object.fields.count(member.name) || object.methods.count(member.name) || member.name == "self")
                        fail(member.location, "invalid or duplicate member '" + member.name + "'");
                    if (member.kind == Stmt::Kind::Function) {
                        object.methods.emplace(member.name, declare_routine(member, stmt.name, section.name == "own"));
                    } else {
                        if (!member.instance_member) fail(member.location, "object fields require 'self'");
                        auto index = static_cast<unsigned>(fields.size());
                        fields.push_back(llvm_type(member.type, member.location));
                        object.fields.emplace(member.name, Field{&member, index, section.name == "own"});
                    }
                }
            }
            object.layout->setBody(fields);
            auto signature = llvm::FunctionType::get(ir.getPtrTy(), false);
            object.constructor = llvm::Function::Create(signature, llvm::Function::InternalLinkage,
                                                        "tell.new." + stmt.name, module);
        }
    }

    void emit_constructor(const Stmt& stmt, Object& object) {
        auto fn = object.constructor;
        ir.SetInsertPoint(llvm::BasicBlock::Create(context, "entry", fn));
        auto ptr = llvm::ConstantPointerNull::get(ir.getPtrTy());
        auto size = ir.CreatePtrToInt(ir.CreateGEP(object.layout, ptr, ir.getInt32(1)), ir.getInt64Ty());
        if (object.fields.empty()) size = ir.getInt64(1);
        auto signature = llvm::FunctionType::get(ir.getPtrTy(), {ir.getInt64Ty()}, false);
        auto instance = ir.CreateCall(module.getOrInsertFunction("malloc", signature), {size});
        auto initialize = llvm::BasicBlock::Create(context, "initialize", fn);
        auto out_of_memory = llvm::BasicBlock::Create(context, "out_of_memory", fn);
        ir.CreateCondBr(ir.CreateIsNull(instance), out_of_memory, initialize);
        ir.SetInsertPoint(out_of_memory);
        ir.CreateCall(module.getOrInsertFunction("abort", llvm::FunctionType::get(ir.getVoidTy(), false)));
        ir.CreateUnreachable();
        ir.SetInsertPoint(initialize);
        for (const auto& section : stmt.body) {
            for (const auto& member : section.body) {
                if (member.kind != Stmt::Kind::Let) continue;
                auto value = expression(*member.expression, member.type);
                require_type(value, member.type, member.expression->location);
                auto address = ir.CreateStructGEP(object.layout, instance, object.fields.at(member.name).index);
                ir.CreateStore(value.value, address);
            }
        }
        ir.CreateRet(instance);
    }

    void emit_if(const Stmt& stmt) {
        auto fn = ir.GetInsertBlock()->getParent();
        auto merge = llvm::BasicBlock::Create(context, "if.end", fn);
        bool falls_through = false;
        bool has_else = false;
        auto emit_arm = [&](const Stmt& arm) {
            if (arm.expression) {
                auto condition = expression(*arm.expression);
                require_type(condition, "bool", arm.expression->location);
                auto yes = llvm::BasicBlock::Create(context, "if.then", fn);
                auto no = llvm::BasicBlock::Create(context, "if.next", fn);
                ir.CreateCondBr(condition.value, yes, no);
                ir.SetInsertPoint(yes);
                emit_branch(arm.body, merge, falls_through);
                ir.SetInsertPoint(no);
            } else {
                has_else = true;
                emit_branch(arm.body, merge, falls_through);
            }
        };
        emit_arm(stmt);
        for (const auto& branch : stmt.branches) emit_arm(branch);
        if (!has_else) {
            ir.CreateBr(merge);
            falls_through = true;
        }
        if (falls_through) ir.SetInsertPoint(merge);
        else {
            ir.ClearInsertionPoint();
            merge->eraseFromParent();
        }
    }

    void emit_switch(const Stmt& stmt) {
        auto value = expression(*stmt.expression);
        auto fn = ir.GetInsertBlock()->getParent();
        auto merge = llvm::BasicBlock::Create(context, "switch.end", fn);
        bool falls_through = false;
        for (const auto& arm : stmt.branches) {
            auto candidate = expression(*arm.expression);
            auto equal = equal_values(value, candidate, arm.expression->location);
            auto yes = llvm::BasicBlock::Create(context, "switch.case", fn);
            auto next = llvm::BasicBlock::Create(context, "switch.next", fn);
            ir.CreateCondBr(equal, yes, next);
            ir.SetInsertPoint(yes);
            emit_branch(arm.body, merge, falls_through);
            ir.SetInsertPoint(next);
        }
        ir.CreateBr(merge);
        ir.SetInsertPoint(merge);
    }

    void emit_branch(const std::vector<Stmt>& statements, llvm::BasicBlock* merge, bool& falls_through) {
        auto outer = variables;
        emit_body(statements);
        variables = std::move(outer);
        if (ir.GetInsertBlock()) {
            ir.CreateBr(merge);
            falls_through = true;
        }
    }

    void emit_body(const std::vector<Stmt>& statements) {
        for (const auto& stmt : statements) emit_statement(stmt);
    }

    void emit_statement(const Stmt& stmt) {
        if (!ir.GetInsertBlock()) fail(stmt.location, "unreachable statement");
        switch (stmt.kind) {
        case Stmt::Kind::Let: {
            if (stmt.instance_member) fail(stmt.location, "'self' fields belong in an object section");
            if (stmt.name == "self" || variables.count(stmt.name))
                fail(stmt.location, "invalid or duplicate variable '" + stmt.name + "'");
            llvm_type(stmt.type, stmt.location);
            auto value = expression(*stmt.expression, stmt.type);
            require_type(value, stmt.type, stmt.expression->location);
            auto slot = entry_alloca(llvm_type(stmt.type, stmt.location));
            ir.CreateStore(value.value, slot);
            variables.emplace(stmt.name, Value{slot, stmt.type, true, stmt.constant});
            break;
        }
        case Stmt::Kind::Raise: {
            auto message = expression(*stmt.expression);
            require_type(message, "text", stmt.expression->location);
            ir.CreateStore(message.value, pending_error);
            propagate_error();
            ir.ClearInsertionPoint();
            break;
        }
        case Stmt::Kind::Try: {
            auto fn = ir.GetInsertBlock()->getParent();
            auto caught = llvm::BasicBlock::Create(context, "try.error", fn);
            auto done = llvm::BasicBlock::Create(context, "try.end", fn);
            auto outer = variables;
            handlers.push_back(caught);
            emit_body(stmt.body);
            handlers.pop_back();
            variables = outer;
            bool falls_through = ir.GetInsertBlock() != nullptr;
            if (falls_through) ir.CreateBr(done);
            ir.SetInsertPoint(caught);
            if (stmt.branches.empty()) {
                propagate_error();
            } else {
                auto message = ir.CreateLoad(ir.getPtrTy(), pending_error);
                ir.CreateStore(llvm::ConstantPointerNull::get(ir.getPtrTy()), pending_error);
                variables["error_message"] = Value{message, "text", false, true};
                emit_body(stmt.branches[0].body);
                variables = outer;
                if (ir.GetInsertBlock()) { ir.CreateBr(done); falls_through = true; }
            }
            if (falls_through) ir.SetInsertPoint(done);
            else { ir.ClearInsertionPoint(); done->eraseFromParent(); }
            break;
        }
        case Stmt::Kind::Set: {
            if (stmt.target) {
                auto address = index_address(*stmt.target);
                auto value = expression(*stmt.expression, address.type);
                require_type(value, address.type, stmt.expression->location);
                ir.CreateStore(value.value, address.value);
                break;
            }
            auto it = variables.find(stmt.name);
            if (it == variables.end()) fail(stmt.location, "unknown variable '" + stmt.name + "'");
            if (it->second.constant) fail(stmt.location, "cannot assign to constant '" + stmt.name + "'");
            if (!it->second.slot) fail(stmt.location, "cannot assign to loop variable '" + stmt.name + "'");
            auto value = expression(*stmt.expression, it->second.type);
            require_type(value, it->second.type, stmt.expression->location);
            ir.CreateStore(value.value, it->second.value);
            break;
        }
        case Stmt::Kind::Repeat: {
            auto count = expression(*stmt.expression);
            require_type(count, "number", stmt.expression->location);
            auto fn = ir.GetInsertBlock()->getParent();
            auto preheader = ir.GetInsertBlock();
            auto condition = llvm::BasicBlock::Create(context, "repeat.condition", fn);
            auto body = llvm::BasicBlock::Create(context, "repeat.body", fn);
            auto step = llvm::BasicBlock::Create(context, "repeat.step", fn);
            auto done = llvm::BasicBlock::Create(context, "repeat.end", fn);
            ir.CreateBr(condition);
            ir.SetInsertPoint(condition);
            auto index = ir.CreatePHI(ir.getInt64Ty(), 2);
            index->addIncoming(ir.getInt64(0), preheader);
            ir.CreateCondBr(ir.CreateICmpSLT(index, count.value), body, done);
            ir.SetInsertPoint(body);
            auto outer = variables;
            loops.emplace_back(done, step);
            emit_body(stmt.body);
            loops.pop_back();
            variables = std::move(outer);
            if (ir.GetInsertBlock()) ir.CreateBr(step);
            ir.SetInsertPoint(step);
            auto next = ir.CreateAdd(index, ir.getInt64(1));
            ir.CreateBr(condition);
            index->addIncoming(next, step);
            ir.SetInsertPoint(done);
            break;
        }
        case Stmt::Kind::While: {
            auto fn = ir.GetInsertBlock()->getParent();
            auto condition = llvm::BasicBlock::Create(context, "while.condition", fn);
            auto body = llvm::BasicBlock::Create(context, "while.body", fn);
            auto done = llvm::BasicBlock::Create(context, "while.end", fn);
            ir.CreateBr(condition);
            ir.SetInsertPoint(condition);
            auto test = expression(*stmt.expression);
            require_type(test, "bool", stmt.expression->location);
            ir.CreateCondBr(test.value, body, done);
            ir.SetInsertPoint(body);
            auto outer = variables;
            loops.emplace_back(done, condition);
            emit_body(stmt.body);
            loops.pop_back();
            variables = std::move(outer);
            if (ir.GetInsertBlock()) ir.CreateBr(condition);
            ir.SetInsertPoint(done);
            break;
        }
        case Stmt::Kind::Say:
            if (stmt.expression->kind == Expr::Kind::String) print_string(*stmt.expression);
            else print_value(expression(*stmt.expression), stmt.expression->location);
            ir.CreateCall(printf_fn, {text("\n")});
            break;
        case Stmt::Kind::Expression:
            expression(*stmt.expression);
            break;
        case Stmt::Kind::For: {
            if (stmt.name == "self" || variables.count(stmt.name))
                fail(stmt.location, "invalid or duplicate variable '" + stmt.name + "'");
            auto list = expression(*stmt.expression);
            if (!is_list(list.type)) fail(stmt.expression->location, "for each requires a list or vector");
            auto layout = list_layout();
            auto length = ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(layout, list.value, 0));
            auto data = ir.CreateLoad(ir.getPtrTy(), ir.CreateStructGEP(layout, list.value, 1));
            auto fn = ir.GetInsertBlock()->getParent();
            auto preheader = ir.GetInsertBlock();
            auto condition = llvm::BasicBlock::Create(context, "for.condition", fn);
            auto body = llvm::BasicBlock::Create(context, "for.body", fn);
            auto step = llvm::BasicBlock::Create(context, "for.step", fn);
            auto done = llvm::BasicBlock::Create(context, "for.end", fn);
            ir.CreateBr(condition);
            ir.SetInsertPoint(condition);
            auto index = ir.CreatePHI(ir.getInt64Ty(), 2);
            index->addIncoming(ir.getInt64(0), preheader);
            ir.CreateCondBr(ir.CreateICmpULT(index, length), body, done);
            ir.SetInsertPoint(body);
            auto type = list.type.substr(8);
            auto element_type = llvm_type(type, stmt.location);
            auto value = ir.CreateLoad(element_type, ir.CreateGEP(element_type, data, index));
            auto outer = variables;
            variables.emplace(stmt.name, Value{value, type});
            loops.emplace_back(done, step);
            emit_body(stmt.body);
            loops.pop_back();
            variables = std::move(outer);
            if (ir.GetInsertBlock()) ir.CreateBr(step);
            ir.SetInsertPoint(step);
            auto next = ir.CreateAdd(index, ir.getInt64(1));
            ir.CreateBr(condition);
            index->addIncoming(next, step);
            ir.SetInsertPoint(done);
            break;
        }
        case Stmt::Kind::Break:
        case Stmt::Kind::Continue:
            if (loops.empty()) fail(stmt.location, "break and continue require a loop");
            ir.CreateBr(stmt.kind == Stmt::Kind::Break ? loops.back().first : loops.back().second);
            ir.ClearInsertionPoint();
            break;
        case Stmt::Kind::Switch:
            emit_switch(stmt);
            break;
        case Stmt::Kind::If:
            emit_if(stmt);
            break;
        case Stmt::Kind::Return: {
            if (!current_routine) fail(stmt.location, "return must be inside a function");
            const auto& type = current_routine->syntax->return_type;
            if (type == "void") {
                if (stmt.expression) fail(stmt.location, "void functions cannot return a value");
                ir.CreateRetVoid();
            } else {
                if (!stmt.expression) fail(stmt.location, "expected a return value of type '" + type + "'");
                auto value = expression(*stmt.expression, type);
                require_type(value, type, stmt.expression->location);
                ir.CreateRet(value.value);
            }
            ir.ClearInsertionPoint();
            break;
        }
        default:
            fail(stmt.location, "statement is not yet supported by the executable backend");
        }
    }

    void emit_routine(const Routine& routine, const std::string& owner) {
        ir.SetInsertPoint(llvm::BasicBlock::Create(context, "entry", routine.function));
        auto arg = routine.function->arg_begin();
        if (!owner.empty()) receiver = {&*arg++, owner};
        for (const auto& parameter : routine.syntax->parameters) {
            auto slot = ir.CreateAlloca(llvm_type(parameter.type, parameter.location));
            ir.CreateStore(&*arg++, slot);
            variables.emplace(parameter.name, Value{slot, parameter.type, true});
        }
        current_routine = &routine;
        emit_body(routine.syntax->body);
        if (ir.GetInsertBlock()) {
            if (routine.syntax->return_type != "void")
                fail(routine.syntax->location, "function must return a value on every path");
            ir.CreateRetVoid();
        }
        variables.clear();
        receiver = {nullptr, ""};
        current_routine = nullptr;
    }

public:
    Generator() {
        auto signature = llvm::FunctionType::get(ir.getInt32Ty(), {ir.getPtrTy()}, true);
        printf_fn = module.getOrInsertFunction("printf", signature);
        pending_error = new llvm::GlobalVariable(module, ir.getPtrTy(), false, llvm::GlobalValue::InternalLinkage, llvm::ConstantPointerNull::get(ir.getPtrTy()), "tell.error");
        input_stream = new llvm::GlobalVariable(module, ir.getPtrTy(), false, llvm::GlobalValue::InternalLinkage, llvm::ConstantPointerNull::get(ir.getPtrTy()), "tell.input");
    }

    void generate(const Program& program, const std::string& path, bool check_only = false
#ifdef TELL_EMBEDDED_CODEGEN
                  , llvm::SmallVectorImpl<char>* object = nullptr, const std::string& target_triple = {}
#endif
                  ) {
        declare(program);
        for (const auto& stmt : program.statements) {
            if (stmt.kind != Stmt::Kind::Object) continue;
            auto& object = objects.at(stmt.name);
            emit_constructor(stmt, object);
            for (const auto& entry : object.methods) emit_routine(entry.second, stmt.name);
        }
        for (const auto& entry : functions) emit_routine(entry.second, "");
        auto fn = llvm::Function::Create(llvm::FunctionType::get(ir.getInt32Ty(), false),
                                        llvm::Function::ExternalLinkage, "main", module);
        ir.SetInsertPoint(llvm::BasicBlock::Create(context, "entry", fn));
        for (const auto& stmt : program.statements) {
            if (stmt.kind != Stmt::Kind::Object && stmt.kind != Stmt::Kind::Function &&
                stmt.kind != Stmt::Kind::ForeignFunction)
                emit_statement(stmt);
        }
        ir.CreateRet(ir.getInt32(0));
        if(llvm::verifyModule(module,&llvm::errs())) throw std::runtime_error("generated invalid LLVM IR");
        if (check_only) return;
#ifdef TELL_EMBEDDED_CODEGEN
        if (object) {
#ifdef TELL_STANDALONE_LINUX
            llvm::InitializeAllTargetInfos();
            llvm::InitializeAllTargets();
            llvm::InitializeAllTargetMCs();
            llvm::InitializeAllAsmPrinters();
#else
            llvm::InitializeNativeTarget();
            llvm::InitializeNativeTargetAsmPrinter();
#endif
            llvm::Triple triple(target_triple);
            std::string error;
            const auto* target = llvm::TargetRegistry::lookupTarget(triple, error);
            if (!target) throw std::runtime_error("cannot select LLVM target: " + error);
            llvm::TargetOptions options;
            std::unique_ptr<llvm::TargetMachine> machine(target->createTargetMachine(
                triple, "generic", "", options, llvm::Reloc::PIC_));
            if (!machine) throw std::runtime_error("cannot create LLVM target machine");
            module.setTargetTriple(triple);
            module.setDataLayout(machine->createDataLayout());
            llvm::raw_svector_ostream out(*object);
            llvm::legacy::PassManager passes;
            if (machine->addPassesToEmitFile(passes, out, nullptr, llvm::CodeGenFileType::ObjectFile))
                throw std::runtime_error("LLVM cannot emit object code for this target");
            passes.run(module);
            return;
        }
#endif
        std::error_code ec;llvm::raw_fd_ostream out(path,ec);if(ec) throw std::runtime_error("cannot write temporary LLVM IR: "+ec.message());module.print(out,nullptr);out.flush();if(out.has_error()) throw std::runtime_error("cannot write temporary LLVM IR");
    }
};
} // namespace

#ifdef TELL_EMBEDDED_CODEGEN
void generate_object(const Program& program, llvm::SmallVectorImpl<char>& object, const std::string& target_triple) {
    Generator{}.generate(program, "", false, &object, target_triple);
}
#endif
void check_program(const Program& program) {
    Generator{}.generate(program, "", true);
}
void generate_ir(const Program& program, const std::string& path) {
    Generator{}.generate(program, path);
}
} // namespace tell
