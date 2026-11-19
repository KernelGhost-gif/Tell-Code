#include "codegen.hpp"
#include "hashmap_codegen.hpp"
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
#include <functional>
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
    llvm::Module& module;
    llvm::LLVMContext& context;
#if LLVM_VERSION_MAJOR >= 24
    llvm::IRBuilder<> ir{module};
#else
    llvm::IRBuilder<> ir{context};
#endif
    HashmapCodegen hashmaps{module, ir};
    std::map<std::string, Value> variables;
    llvm::FunctionCallee printf_fn;
    std::map<std::string, Object> objects;
    std::map<std::string, Routine> functions;
    std::map<std::string, const Stmt*> foreign_functions;
    Value receiver{nullptr, ""};
    std::vector<std::pair<llvm::BasicBlock*, llvm::BasicBlock*>> loops;
    bool is_list(const std::string& type) { return type.rfind("list of ", 0) == 0; }
    bool is_set(const std::string& type) { return type.rfind("set of ", 0) == 0; }
    bool is_tuple(const std::string& type) { return type.rfind("tuple of (", 0) == 0; }

    std::vector<std::string> tuple_types(const std::string& type) {
        std::vector<std::string> types;
        size_t start = 10;
        int depth = 0;
        for (size_t i = start; i + 1 < type.size(); ++i) {
            if (type[i] == '(') ++depth;
            if (type[i] == ')') --depth;
            if (type[i] == ',' && depth == 0) {
                types.push_back(type.substr(start, i - start));
                start = ++i + 1;
            }
        }
        if (start + 1 < type.size()) types.push_back(type.substr(start, type.size() - start - 1));
        return types;
    }

    llvm::StructType* tuple_layout(const std::vector<std::string>& types, Location loc) {
        std::vector<llvm::Type*> fields;
        for (const auto& type : types) fields.push_back(llvm_type(type, loc));
        return llvm::StructType::get(context, fields);
    }

    Value tuple_value(const std::vector<Value>& values, Location loc) {
        std::vector<std::string> types;
        std::string type = "tuple of (";
        for (const auto& value : values) {
            if (value.type == "void") fail(loc, "tuple element cannot be void");
            if (!types.empty()) type += ", ";
            types.push_back(value.type);
            type += value.type;
        }
        auto layout = tuple_layout(types, loc);
        auto null = llvm::ConstantPointerNull::get(ir.getPtrTy());
        auto size = values.empty() ? ir.getInt64(1) :
            ir.CreatePtrToInt(ir.CreateGEP(layout, null, ir.getInt64(1)), ir.getInt64Ty());
        auto storage = allocate(size);
        for (size_t i = 0; i < values.size(); ++i)
            ir.CreateStore(values[i].value, ir.CreateStructGEP(layout, storage, i));
        return {storage, type + ")"};
    }

    Value tuple_item(Value tuple, const std::vector<std::string>& types, size_t index, Location loc) {
        auto slot = ir.CreateStructGEP(tuple_layout(types, loc), tuple.value, index);
        return {ir.CreateLoad(llvm_type(types[index], loc), slot), types[index]};
    }
    llvm::StructType* list_layout() { return llvm::StructType::get(context, {ir.getInt64Ty(), ir.getPtrTy(), ir.getInt64Ty()}); }
    static constexpr llvm::StringLiteral hashmap_prefix = "hashmap of ";
    bool is_hashmap(const std::string& type) { return llvm::StringRef(type).starts_with(hashmap_prefix); }
    std::pair<std::string, std::string> hashmap_types(const std::string& type) {
        auto types = llvm::StringRef(type).drop_front(hashmap_prefix.size()).split(" to ");
        return {types.first.str(), types.second.str()};
    }
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
    std::pair<llvm::Value*, llvm::Value*> allocate_list(llvm::Type* element, llvm::Value* length) {
        auto null = llvm::ConstantPointerNull::get(ir.getPtrTy());
        auto capacity = ir.CreateSelect(ir.CreateICmpEQ(length, ir.getInt64(0)), ir.getInt64(1), length);
        auto data_size = ir.CreatePtrToInt(ir.CreateGEP(element, null, capacity), ir.getInt64Ty());
        auto data = allocate(data_size);
        auto layout = list_layout();
        auto header_size = ir.CreatePtrToInt(ir.CreateGEP(layout, null, ir.getInt64(1)), ir.getInt64Ty());
        auto header = allocate(header_size);
        ir.CreateStore(length, ir.CreateStructGEP(layout, header, 0));
        ir.CreateStore(data, ir.CreateStructGEP(layout, header, 1));
        ir.CreateStore(capacity, ir.CreateStructGEP(layout, header, 2));
        return {header, data};
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
        if (is_list(type) || is_set(type)) { llvm_type(type.substr(is_set(type) ? 7 : 8), loc); return ir.getPtrTy(); }
        if (is_tuple(type)) { tuple_layout(tuple_types(type), loc); return ir.getPtrTy(); }
        if (is_hashmap(type)) { llvm_type(hashmap_types(type).second, loc); return ir.getPtrTy(); }
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

    Value call(const Expr& expr, const std::string& expected) {
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
            if (callee.value == "len") {
                if (expr.children.size() != 2) fail(expr.location, "len requires one argument");
                auto collection = expression(*expr.children[1]);
                if (is_hashmap(collection.type)) return {hashmaps.length(collection.value), "number"};
                if (is_tuple(collection.type)) return {ir.getInt64(tuple_types(collection.type).size()), "number"};
                if (!is_list(collection.type) && !is_set(collection.type)) fail(expr.location, "len requires a list, tuple, set, or hashmap");
                return {ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(list_layout(), collection.value, 0)), "number"};
            }
            if (callee.value == "set" || callee.value == "union" || callee.value == "intersection" ||
                callee.value == "difference" || callee.value == "first" || callee.value == "last" ||
                callee.value == "reversed" || callee.value == "enumerate")
                return collection_call(expr, callee.value, expected);
            if (callee.value == "map" || callee.value == "filter" || callee.value == "reduce")
                return transform_list(expr, callee.value);
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
                if (function == functions.end()) {
                    if (callee.value == "keys") return hashmap_keys(expr);
                    fail(callee.location, "unknown function '" + callee.value + "'");
                }
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
        if (is_tuple(lhs.type)) {
            auto types = tuple_types(lhs.type);
            llvm::Value* equal = ir.getTrue();
            for (size_t i = 0; i < types.size(); ++i)
                equal = ir.CreateAnd(equal, equal_values(tuple_item(lhs, types, i, loc), tuple_item(rhs, types, i, loc), loc));
            return equal;
        }
        if (is_set(lhs.type)) {
            auto length = list_length(lhs);
            auto index = find_index(length, [&](llvm::Value* i) {
                return ir.CreateNot(sequence_contains(rhs, list_item(lhs, i, lhs.type.substr(7), loc), loc));
            });
            return ir.CreateAnd(ir.CreateICmpEQ(index, length), ir.CreateICmpEQ(length, list_length(rhs)));
        }
        if (lhs.type == "decimal") return ir.CreateFCmpOEQ(lhs.value, rhs.value);
        return ir.CreateICmpEQ(lhs.value, rhs.value);
    }
    Value index_address(const Expr& e, Value list, Value index) {
        if (!is_list(list.type)) fail(e.location, "indexing requires a list, vector, or hashmap");
        require_type(index, "number", e.children[1]->location);
        auto layout = list_layout();
        auto length = ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(layout, list.value, 0));
        raise_if(ir.CreateICmpUGE(index.value, length), "index out of bounds");
        auto data = ir.CreateLoad(ir.getPtrTy(), ir.CreateStructGEP(layout, list.value, 1));
        auto type = list.type.substr(8);
        return {ir.CreateGEP(llvm_type(type, e.location), data, index.value), type};
    }

    llvm::Value* hashmap_key(Value key) {
        if (key.type == "text") return ir.CreatePtrToInt(key.value, ir.getInt64Ty());
        if (key.type == "decimal") {
            raise_if(ir.CreateFCmpUNO(key.value, key.value), "NaN cannot be a hashmap key");
            auto zero = llvm::ConstantFP::get(ir.getDoubleTy(), 0);
            auto bits = ir.CreateBitCast(key.value, ir.getInt64Ty());
            return ir.CreateSelect(ir.CreateFCmpOEQ(key.value, zero), ir.getInt64(0), bits);
        }
        return ir.CreateZExtOrTrunc(key.value, ir.getInt64Ty());
    }

    Value hashmap_keys(const Expr& expr) {
        if (expr.children.size() != 2) fail(expr.location, "keys requires one argument");
        auto table = expression(*expr.children[1]);
        if (!is_hashmap(table.type)) fail(expr.children[1]->location, "keys requires a hashmap");
        auto key_type = hashmap_types(table.type).first;
        auto element = llvm_type(key_type, expr.location);
        auto [list, data] = allocate_list(element, hashmaps.length(table.value));
        hashmaps.copy_keys(table.value, data, element);
        return {list, "list of " + key_type};
    }

    void hashmap_store(llvm::Value* table, Value key, Value value) {
        auto entry = hashmaps.insert(table, hashmap_key(key), key.type == "text");
        raise_if(ir.CreateIsNull(entry), "cannot insert into hashmap");
        ir.CreateStore(value.value, hashmaps.value_address(entry));
    }

    Value hashmap_literal(const Expr& expr, const std::string& expected) {
        auto types = is_hashmap(expected) ? hashmap_types(expected) : std::pair<std::string, std::string>{};
        auto& [key_type, value_type] = types;
        if (expr.children.empty() && key_type.empty())
            fail(expr.location, "empty hashmap requires an explicit hashmap type");

        auto table = hashmaps.create();
        raise_if(ir.CreateIsNull(table), "cannot allocate hashmap");
        for (size_t i = 0; i < expr.children.size(); i += 2) {
            const auto& key_expr = *expr.children[i];
            const auto& value_expr = *expr.children[i + 1];
            auto key = expression(key_expr);
            if (key_type.empty()) {
                if (key.type != "text" && key.type != "number" && key.type != "bool" && key.type != "decimal")
                    fail(key_expr.location, "hashmap key type must be text, number, bool, or decimal");
                key_type = key.type;
            }
            require_type(key, key_type, key_expr.location);
            auto value = expression(value_expr, value_type);
            if (value_type.empty()) value_type = value.type;
            if (value_type == "void") fail(value_expr.location, "hashmap value type cannot be void");
            require_type(value, value_type, value_expr.location);
            hashmap_store(table, key, value);
        }
        return {table, hashmap_prefix.str() + key_type + " to " + value_type};
    }

    llvm::Value* list_length(Value list) {
        return ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(list_layout(), list.value, 0));
    }

    Value list_item(Value list, llvm::Value* index, const std::string& type, Location loc) {
        auto data = ir.CreateLoad(ir.getPtrTy(), ir.CreateStructGEP(list_layout(), list.value, 1));
        auto element = llvm_type(type, loc);
        return {ir.CreateLoad(element, ir.CreateGEP(element, data, index)), type};
    }

    // Emit a loop whose callback returns true to stop at the current index.
    llvm::Value* find_index(llvm::Value* length, const std::function<llvm::Value*(llvm::Value*)>& stop) {
        auto start = ir.GetInsertBlock();
        auto fn = start->getParent();
        auto check = llvm::BasicBlock::Create(context, "scan.check", fn);
        auto body = llvm::BasicBlock::Create(context, "scan.body", fn);
        auto next = llvm::BasicBlock::Create(context, "scan.next", fn);
        auto done = llvm::BasicBlock::Create(context, "scan.end", fn);
        ir.CreateBr(check);
        ir.SetInsertPoint(check);
        auto index = ir.CreatePHI(ir.getInt64Ty(), 2);
        index->addIncoming(ir.getInt64(0), start);
        ir.CreateCondBr(ir.CreateICmpULT(index, length), body, done);
        ir.SetInsertPoint(body);
        auto found = stop(index);
        ir.CreateCondBr(found, done, next);
        ir.SetInsertPoint(next);
        auto increment = ir.CreateAdd(index, ir.getInt64(1));
        ir.CreateBr(check);
        index->addIncoming(increment, next);
        ir.SetInsertPoint(done);
        return index;
    }

    llvm::Value* sequence_contains(Value list, Value value, Location loc) {
        auto type = list.type.substr(is_set(list.type) ? 7 : 8);
        require_type(value, type, loc);
        auto length = list_length(list);
        auto index = find_index(length, [&](llvm::Value* i) {
            return equal_values(list_item(list, i, type, loc), value, loc);
        });
        return ir.CreateICmpULT(index, length);
    }

    void append_item(Value list, Value value, Location loc) {
        auto layout = list_layout();
        auto length_ptr = ir.CreateStructGEP(layout, list.value, 0);
        auto data_ptr = ir.CreateStructGEP(layout, list.value, 1);
        auto capacity_ptr = ir.CreateStructGEP(layout, list.value, 2);
        auto length = ir.CreateLoad(ir.getInt64Ty(), length_ptr);
        auto element_type = llvm_type(value.type, loc);
        auto null = llvm::ConstantPointerNull::get(ir.getPtrTy());
        auto element_size = ir.CreatePtrToInt(ir.CreateGEP(element_type, null, ir.getInt64(1)), ir.getInt64Ty());
        auto fn = ir.GetInsertBlock()->getParent();
        auto capacity = ir.CreateLoad(ir.getInt64Ty(), capacity_ptr);
        auto grow = llvm::BasicBlock::Create(context, "list.grow", fn);
        auto ready = llvm::BasicBlock::Create(context, "list.ready", fn);
        ir.CreateCondBr(ir.CreateICmpEQ(length, capacity), grow, ready);
        ir.SetInsertPoint(grow);
        auto max_capacity = ir.CreateUDiv(ir.getInt64(INT64_MAX), element_size);
        raise_if(ir.CreateICmpUGT(capacity, ir.CreateUDiv(max_capacity, ir.getInt64(2))), "list too large");
        auto new_capacity = ir.CreateMul(capacity, ir.getInt64(2));
        auto data = ir.CreateLoad(ir.getPtrTy(), data_ptr);
        auto signature = llvm::FunctionType::get(ir.getPtrTy(), {ir.getPtrTy(), ir.getInt64Ty()}, false);
        auto resized = ir.CreateCall(module.getOrInsertFunction("realloc", signature), {data, ir.CreateMul(new_capacity, element_size)});
        raise_if(ir.CreateIsNull(resized), "cannot grow list");
        ir.CreateStore(resized, data_ptr);
        ir.CreateStore(new_capacity, capacity_ptr);
        ir.CreateBr(ready);
        ir.SetInsertPoint(ready);
        auto current = ir.CreateLoad(ir.getPtrTy(), data_ptr);
        ir.CreateStore(value.value, ir.CreateGEP(element_type, current, length));
        ir.CreateStore(ir.CreateAdd(length, ir.getInt64(1)), length_ptr);
    }

    void insert_set(Value set, Value value, Location loc) {
        auto found = sequence_contains(set, value, loc);
        auto fn = ir.GetInsertBlock()->getParent();
        auto insert = llvm::BasicBlock::Create(context, "set.insert", fn);
        auto done = llvm::BasicBlock::Create(context, "set.end", fn);
        ir.CreateCondBr(found, done, insert);
        ir.SetInsertPoint(insert);
        append_item(set, value, loc);
        ir.CreateBr(done);
        ir.SetInsertPoint(done);
    }

    std::vector<Value> unpack(Value sequence, size_t count, Location loc) {
        std::vector<Value> values;
        if (is_tuple(sequence.type)) {
            auto types = tuple_types(sequence.type);
            if (types.size() != count) fail(loc, "destructuring arity mismatch");
            for (size_t i = 0; i < count; ++i) values.push_back(tuple_item(sequence, types, i, loc));
        } else if (is_list(sequence.type)) {
            raise_if(ir.CreateICmpNE(list_length(sequence), ir.getInt64(count)), "destructuring arity mismatch");
            for (size_t i = 0; i < count; ++i)
                values.push_back(list_item(sequence, ir.getInt64(i), sequence.type.substr(8), loc));
        } else fail(loc, "destructuring requires a list or tuple");
        return values;
    }

    void bind_names(const Stmt& stmt, const std::vector<Value>& values, bool loop = false) {
        std::set<std::string> unique;
        for (size_t i = 0; i < values.size(); ++i) {
            const auto& name = stmt.names[i];
            if (!unique.insert(name).second || name == "self") fail(stmt.location, "invalid or duplicate variable '" + name + "'");
            auto it = variables.find(name);
            if (stmt.kind == Stmt::Kind::Set) {
                if (it == variables.end()) fail(stmt.location, "unknown variable '" + name + "'");
                if (!it->second.slot || it->second.constant) fail(stmt.location, "cannot assign to '" + name + "'");
                require_type(values[i], it->second.type, stmt.location);
            } else if (it != variables.end()) fail(stmt.location, "invalid or duplicate variable '" + name + "'");
        }
        for (size_t i = 0; i < values.size(); ++i) {
            const auto& name = stmt.names[i];
            const auto& value = values[i];
            if (stmt.kind == Stmt::Kind::Set) ir.CreateStore(value.value, variables.at(name).value);
            else if (loop) variables.emplace(name, value);
            else {
                auto slot = entry_alloca(llvm_type(value.type, stmt.location));
                ir.CreateStore(value.value, slot);
                variables.emplace(name, Value{slot, value.type, true, stmt.constant});
            }
        }
    }

    int64_t constant_index(Value value, Location loc) {
        require_type(value, "number", loc);
        auto index = llvm::dyn_cast<llvm::ConstantInt>(value.value);
        if (!index) fail(loc, "tuple indexes and slice bounds must be constant integers");
        return index->getSExtValue();
    }

    Value slice(Value sequence, const Expr& expr) {
        bool tuple = is_tuple(sequence.type);
        if (!tuple && !is_list(sequence.type)) fail(expr.location, "slicing requires a list or tuple");
        auto types = tuple ? tuple_types(sequence.type) : std::vector<std::string>{};
        llvm::Value* bounds[3]{};
        for (size_t i = 0; i < 3; ++i) {
            if (const auto& child = expr.children[i + 1]) {
                auto value = expression(*child);
                require_type(value, "number", child->location);
                bounds[i] = tuple ? ir.getInt64(constant_index(value, child->location)) : value.value;
            }
        }
        // Bounds may call functions that resize the source list.
        llvm::Value* length = tuple ? static_cast<llvm::Value*>(ir.getInt64(types.size())) : list_length(sequence);
        auto zero = ir.getInt64(0);
        auto one = ir.getInt64(1);
        auto minus_one = ir.getInt64(-1);
        auto step = bounds[2] ? bounds[2] : one;
        if (tuple && llvm::cast<llvm::ConstantInt>(step)->isZero()) fail(expr.location, "slice step cannot be zero");
        if (!tuple) raise_if(ir.CreateICmpEQ(step, zero), "slice step cannot be zero");
        auto backwards = ir.CreateICmpSLT(step, zero);
        auto lower = ir.CreateSelect(backwards, minus_one, zero);
        auto upper = ir.CreateSelect(backwards, ir.CreateSub(length, one), length);
        auto normalize = [&](llvm::Value* bound) {
            auto shifted = ir.CreateSelect(ir.CreateICmpSLT(bound, zero), ir.CreateAdd(bound, length), bound);
            auto clamped = ir.CreateSelect(ir.CreateICmpSLT(shifted, lower), lower, shifted);
            return ir.CreateSelect(ir.CreateICmpSGT(clamped, upper), upper, clamped);
        };
        auto start = bounds[0] ? normalize(bounds[0]) : ir.CreateSelect(backwards, upper, lower);
        auto stop = bounds[1] ? normalize(bounds[1]) : ir.CreateSelect(backwards, lower, upper);
        auto distance = ir.CreateSelect(backwards, ir.CreateSub(start, stop), ir.CreateSub(stop, start));
        auto magnitude = ir.CreateSelect(backwards, ir.CreateNeg(step), step);
        auto count = ir.CreateSelect(ir.CreateICmpSGT(distance, zero),
            ir.CreateAdd(ir.CreateUDiv(ir.CreateSub(distance, one), magnitude), one), zero);
        if (tuple) {
            std::vector<Value> values;
            auto n = llvm::cast<llvm::ConstantInt>(count)->getZExtValue();
            auto index = llvm::cast<llvm::ConstantInt>(start)->getZExtValue();
            auto increment = llvm::cast<llvm::ConstantInt>(step)->getZExtValue();
            for (uint64_t i = 0; i < n; ++i, index += increment)
                values.push_back(tuple_item(sequence, types, index, expr.location));
            return tuple_value(values, expr.location);
        }
        auto type = sequence.type.substr(8);
        auto element = llvm_type(type, expr.location);
        auto storage = allocate_list(element, count);
        auto data = storage.second;
        find_index(count, [&](llvm::Value* index) {
            auto offset = ir.CreateAdd(start, ir.CreateMul(index, step));
            auto value = list_item(sequence, offset, type, expr.location);
            ir.CreateStore(value.value, ir.CreateGEP(element, data, index));
            return ir.getFalse();
        });
        return {storage.first, sequence.type};
    }

    Value transform_list(const Expr& expr, const std::string& operation) {
        bool reduce = operation == "reduce";
        if (expr.children.size() != (reduce ? 4u : 3u))
            fail(expr.location, operation + " requires a function, a list" + (reduce ? ", and an initial value" : ""));
        const auto& callback = *expr.children[1];
        auto routine = functions.find(callback.value);
        if (callback.kind != Expr::Kind::Name || routine == functions.end() || variables.count(callback.value))
            fail(callback.location, "expected a named TellC function");
        auto source = expression(*expr.children[2]);
        if (!is_list(source.type)) fail(expr.children[2]->location, operation + " requires a list");
        auto element = source.type.substr(8);
        const auto& signature = *routine->second.syntax;
        if (signature.parameters.size() != (reduce ? 2u : 1u) ||
            signature.parameters.back().type != element)
            fail(callback.location, "callback parameters do not match the list element type");
        if (operation == "filter" && signature.return_type != "bool")
            fail(callback.location, "filter callback must return bool");
        if (signature.return_type == "void") fail(callback.location, "callback must return a value");

        auto output = operation == "map" ? signature.return_type : element;
        auto output_type = llvm_type(output, expr.location);
        llvm::AllocaInst* accumulator = nullptr;
        if (reduce) {
            auto initial = expression(*expr.children[3], signature.return_type);
            require_type(initial, signature.return_type, expr.children[3]->location);
            if (signature.parameters.front().type != initial.type)
                fail(callback.location, "reduce callback must take and return the accumulator type");
            output_type = llvm_type(initial.type, expr.location);
            accumulator = entry_alloca(output_type);
            ir.CreateStore(initial.value, accumulator);
        }
        auto length = list_length(source);
        auto input_type = llvm_type(element, expr.location);
        auto snapshot_storage = allocate_list(input_type, length);
        auto snapshot = snapshot_storage.first;
        auto snapshot_data = snapshot_storage.second;
        auto input_data = ir.CreateLoad(ir.getPtrTy(), ir.CreateStructGEP(list_layout(), source.value, 1));
        auto bytes = ir.CreatePtrToInt(ir.CreateGEP(input_type, llvm::ConstantPointerNull::get(ir.getPtrTy()), length),
                                       ir.getInt64Ty());
        ir.CreateMemCpy(snapshot_data, llvm::MaybeAlign(), input_data, llvm::MaybeAlign(), bytes);
        Value result{nullptr, "list of " + output};
        if (!reduce) result.value = allocate_list(output_type, ir.getInt64(0)).first;
        find_index(length, [&](llvm::Value* index) {
            auto item = list_item({snapshot, source.type}, index, element, expr.location);
            std::vector<llvm::Value*> args;
            if (reduce) args.push_back(ir.CreateLoad(output_type, accumulator));
            args.push_back(item.value);
            auto value = ir.CreateCall(routine->second.function, args);
            check_pending_error();
            if (reduce) ir.CreateStore(value, accumulator);
            else if (operation == "map") append_item(result, {value, output}, expr.location);
            else {
                auto fn = ir.GetInsertBlock()->getParent();
                auto keep = llvm::BasicBlock::Create(context, "filter.keep", fn);
                auto next = llvm::BasicBlock::Create(context, "filter.next", fn);
                ir.CreateCondBr(value, keep, next);
                ir.SetInsertPoint(keep);
                append_item(result, item, expr.location);
                ir.CreateBr(next);
                ir.SetInsertPoint(next);
            }
            return ir.getFalse();
        });
        if (reduce) return {ir.CreateLoad(output_type, accumulator), signature.return_type};
        return result;
    }

    Value collection_call(const Expr& expr, const std::string& name, const std::string& expected) {
        auto argc = expr.children.size() - 1;
        if (name == "set") {
            if (argc > 1) fail(expr.location, "set takes zero or one argument");
            auto type = is_set(expected) ? expected.substr(7) : "";
            if (!argc) {
                if (type.empty()) fail(expr.location, "empty set requires an explicit set type");
                return {allocate_list(llvm_type(type, expr.location), ir.getInt64(0)).first, expected};
            }
            auto source = expression(*expr.children[1], type.empty() ? "" : "list of " + type);
            if (!is_list(source.type) && !is_set(source.type)) fail(expr.location, "set requires a list or set");
            auto element = source.type.substr(is_set(source.type) ? 7 : 8);
            if (!type.empty() && type != element) fail(expr.location, "set element type mismatch");
            Value result{allocate_list(llvm_type(element, expr.location), ir.getInt64(0)).first, "set of " + element};
            find_index(list_length(source), [&](llvm::Value* index) {
                insert_set(result, list_item(source, index, element, expr.location), expr.location);
                return ir.getFalse();
            });
            return result;
        }
        if (name == "union" || name == "intersection" || name == "difference") {
            if (argc != 2) fail(expr.location, name + " requires two sets");
            auto left = expression(*expr.children[1], expected);
            auto right = expression(*expr.children[2], left.type);
            if (!is_set(left.type)) fail(expr.location, name + " requires two sets");
            require_type(right, left.type, expr.location);
            auto type = left.type.substr(7);
            Value result{allocate_list(llvm_type(type, expr.location), ir.getInt64(0)).first, left.type};
            find_index(list_length(left), [&](llvm::Value* i) {
                auto item = list_item(left, i, type, expr.location);
                if (name == "union") append_item(result, item, expr.location);
                else {
                    auto found = sequence_contains(right, item, expr.location);
                    auto fn = ir.GetInsertBlock()->getParent();
                    auto insert = llvm::BasicBlock::Create(context, "set.keep", fn);
                    auto next = llvm::BasicBlock::Create(context, "set.skip", fn);
                    ir.CreateCondBr(name == "intersection" ? found : ir.CreateNot(found), insert, next);
                    ir.SetInsertPoint(insert);
                    append_item(result, item, expr.location);
                    ir.CreateBr(next);
                    ir.SetInsertPoint(next);
                }
                return ir.getFalse();
            });
            if (name == "union") {
                find_index(list_length(right), [&](llvm::Value* i) {
                    insert_set(result, list_item(right, i, type, expr.location), expr.location);
                    return ir.getFalse();
                });
            }
            return result;
        }
        if (argc != 1) fail(expr.location, name + " requires one collection");
        auto source = expression(*expr.children[1]);
        if (name == "enumerate") {
            if (!is_list(source.type)) fail(expr.location, "enumerate requires a list");
            auto type = source.type.substr(8);
            auto length = list_length(source);
            auto storage = allocate_list(ir.getPtrTy(), length);
            find_index(length, [&](llvm::Value* index) {
                auto pair = tuple_value({{index, "number"}, list_item(source, index, type, expr.location)}, expr.location);
                ir.CreateStore(pair.value, ir.CreateGEP(ir.getPtrTy(), storage.second, index));
                return ir.getFalse();
            });
            return {storage.first, "list of tuple of (number, " + type + ")"};
        }
        if (is_tuple(source.type)) {
            auto types = tuple_types(source.type);
            if (name == "reversed") {
                std::vector<Value> values;
                for (size_t i = types.size(); i > 0; --i)
                    values.push_back(tuple_item(source, types, i - 1, expr.location));
                return tuple_value(values, expr.location);
            }
            if (types.empty()) fail(expr.location, name + " requires a nonempty tuple");
            return tuple_item(source, types, name == "first" ? 0 : types.size() - 1, expr.location);
        }
        if (!is_list(source.type)) fail(expr.location, name + " requires a list or tuple");
        auto type = source.type.substr(8);
        auto length = list_length(source);
        if (name != "reversed") {
            raise_if(ir.CreateICmpEQ(length, ir.getInt64(0)), "collection is empty");
            return list_item(source, name == "first" ? ir.getInt64(0) : ir.CreateSub(length, ir.getInt64(1)), type, expr.location);
        }
        auto element = llvm_type(type, expr.location);
        auto storage = allocate_list(element, length);
        auto data = storage.second;
        find_index(length, [&](llvm::Value* index) {
            auto offset = ir.CreateSub(ir.CreateSub(length, ir.getInt64(1)), index);
            ir.CreateStore(list_item(source, offset, type, expr.location).value, ir.CreateGEP(element, data, index));
            return ir.getFalse();
        });
        return {storage.first, source.type};
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
            auto [header, data] = allocate_list(element_type, ir.getInt64(values.size()));
            for (size_t i = 0; i < values.size(); ++i)
                ir.CreateStore(values[i].value, ir.CreateGEP(element_type, data, ir.getInt64(i)));
            return {header, "list of " + element};
        }
        if (e.kind == K::Tuple) {
            auto types = is_tuple(expected) ? tuple_types(expected) : std::vector<std::string>{};
            if (is_tuple(expected) && types.size() != e.children.size()) fail(e.location, "tuple arity mismatch");
            std::vector<Value> values;
            for (size_t i = 0; i < e.children.size(); ++i)
                values.push_back(expression(*e.children[i], types.empty() ? "" : types[i]));
            return tuple_value(values, e.location);
        }
        if (e.kind == K::Set) {
            auto type = is_set(expected) ? expected.substr(7) : "";
            Value set{nullptr, ""};
            for (const auto& item : e.children) {
                auto value = expression(*item, type);
                if (!set.value) {
                    if (value.type == "void") fail(item->location, "set element cannot be void");
                    if (type.empty()) type = value.type;
                    set = {allocate_list(llvm_type(type, e.location), ir.getInt64(0)).first, "set of " + type};
                }
                insert_set(set, value, item->location);
            }
            return set;
        }
        if (e.kind == K::Hashmap) return hashmap_literal(e, expected);
        if (e.kind == K::Index) {
            auto collection = expression(*e.children[0]);
            auto index = expression(*e.children[1]);
            if (is_tuple(collection.type)) {
                auto types = tuple_types(collection.type);
                require_type(index, "number", e.children[1]->location);
                if (!llvm::isa<llvm::ConstantInt>(index.value) && !types.empty() &&
                    std::all_of(types.begin(), types.end(), [&](const auto& type) { return type == types.front(); })) {
                    auto length = ir.getInt64(types.size());
                    auto offset = ir.CreateSelect(ir.CreateICmpSLT(index.value, ir.getInt64(0)),
                                                  ir.CreateAdd(index.value, length), index.value);
                    raise_if(ir.CreateICmpUGE(offset, length), "tuple index out of bounds");
                    auto element = llvm_type(types.front(), e.location);
                    return {ir.CreateLoad(element, ir.CreateGEP(element, collection.value, offset)), types.front()};
                }
                auto i = constant_index(index, e.children[1]->location);
                if (i < 0) i += static_cast<int64_t>(types.size());
                if (i < 0 || static_cast<size_t>(i) >= types.size()) fail(e.location, "tuple index out of bounds");
                return tuple_item(collection, types, i, e.location);
            }
            if (is_hashmap(collection.type)) {
                auto [key_type, value_type] = hashmap_types(collection.type);
                require_type(index, key_type, e.children[1]->location);
                auto entry = hashmaps.find(collection.value, hashmap_key(index), key_type == "text");
                raise_if(ir.CreateIsNull(entry), "key not found in hashmap");
                return {ir.CreateLoad(llvm_type(value_type, e.location), hashmaps.value_address(entry)), value_type};
            }
            auto address = index_address(e, collection, index);
            return {ir.CreateLoad(llvm_type(address.type, e.location), address.value), address.type};
        }
        if (e.kind == K::Name) return lookup(e.value, e.location);
        if (e.kind == K::Member) return field(expression(*e.children[0]), e.value, e.location);
        if (e.kind == K::Call) return call(e, expected);
        if (e.kind == K::Slice) return slice(expression(*e.children[0]), e);
        auto check_bitwise_range = [&](llvm::Value* value) {
            auto low = ir.CreateICmpSLT(value, ir.getInt64(-9007199254740991LL));
            auto high = ir.CreateICmpSGT(value, ir.getInt64(9007199254740991LL));
            raise_if(ir.CreateOr(low, high), "bitwise value exceeds exact numeric range");
        };
        if (e.kind == K::Unary) {
            auto operand = expression(*e.children[0]);
            auto op = e.value;
            for (auto& c : op) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
            if (op == "not" || op == "!") {
                require_type(operand, "bool", e.location);
                return {ir.CreateNot(operand.value), "bool"};
            }
            if (op == "~") {
                require_type(operand, "number", e.location);
                check_bitwise_range(operand.value);
                auto result = ir.CreateNot(operand.value);
                check_bitwise_range(result);
                return {result, "number"};
            }
            if (op == "+" || op == "-") {
                if (operand.type == "number") return {op == "-" ? ir.CreateNeg(operand.value) : operand.value, "number"};
                if (operand.type == "decimal") return {op == "-" ? ir.CreateFNeg(operand.value) : operand.value, "decimal"};
            }
            fail(e.location, "invalid unary operator or operand");
        }
        if (e.kind == K::Binary) {
            auto lhs = expression(*e.children[0]);
            auto op = e.value;
            for (auto& c : op) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
            if (op == "and" || op == "&&" || op == "or" || op == "||") {
                require_type(lhs, "bool", e.location);
                bool is_and = op == "and" || op == "&&";
                auto fn = ir.GetInsertBlock()->getParent();
                auto from = ir.GetInsertBlock();
                auto rhs_block = llvm::BasicBlock::Create(context, "logic.rhs", fn);
                auto merge = llvm::BasicBlock::Create(context, "logic.end", fn);
                if (is_and) ir.CreateCondBr(lhs.value, rhs_block, merge);
                else ir.CreateCondBr(lhs.value, merge, rhs_block);
                ir.SetInsertPoint(rhs_block);
                auto rhs = expression(*e.children[1]);
                require_type(rhs, "bool", e.children[1]->location);
                auto rhs_end = ir.GetInsertBlock();
                ir.CreateBr(merge);
                ir.SetInsertPoint(merge);
                auto result = ir.CreatePHI(ir.getInt1Ty(), 2);
                result->addIncoming(ir.getInt1(!is_and), from);
                result->addIncoming(rhs.value, rhs_end);
                return {result, "bool"};
            }
            auto rhs = expression(*e.children[1]);
            if (op == "in") {
                if (is_list(rhs.type) || is_set(rhs.type)) return {sequence_contains(rhs, lhs, e.location), "bool"};
                if (is_tuple(rhs.type)) {
                    auto types = tuple_types(rhs.type);
                    llvm::Value* found = ir.getFalse();
                    for (size_t i = 0; i < types.size(); ++i) {
                        if (lhs.type == types[i]) found = ir.CreateOr(found, equal_values(lhs, tuple_item(rhs, types, i, e.location), e.location));
                    }
                    return {found, "bool"};
                }
                if (!is_hashmap(rhs.type)) fail(e.children[1]->location, "'in' requires a list, tuple, set, or hashmap on the right");
                auto key_type = hashmap_types(rhs.type).first;
                require_type(lhs, key_type, e.children[0]->location);
                auto entry = hashmaps.find(rhs.value, hashmap_key(lhs), key_type == "text");
                return {ir.CreateIsNotNull(entry), "bool"};
            }
            require_type(rhs, lhs.type, e.children[1]->location);
            if (e.value == "=" || e.value == "==" || e.value == "!=") {
                auto equal = equal_values(lhs, rhs, e.children[1]->location);
                return {e.value == "!=" ? ir.CreateNot(equal) : equal, "bool"};
            }
            if (lhs.type == "number") {
                if (e.value == "&" || e.value == "|" || e.value == "^" || e.value == "<<" || e.value == ">>") {
                    check_bitwise_range(lhs.value);
                    check_bitwise_range(rhs.value);
                    llvm::Value* result = nullptr;
                    if (e.value == "&") result = ir.CreateAnd(lhs.value, rhs.value);
                    else if (e.value == "|") result = ir.CreateOr(lhs.value, rhs.value);
                    else if (e.value == "^") result = ir.CreateXor(lhs.value, rhs.value);
                    else {
                        auto bad = ir.CreateOr(ir.CreateICmpSLT(rhs.value, ir.getInt64(0)), ir.CreateICmpSGE(rhs.value, ir.getInt64(64)));
                        raise_if(bad, "shift count must be between 0 and 63");
                        if (e.value == "<<") {
                            result = ir.CreateShl(lhs.value, rhs.value);
                            raise_if(ir.CreateICmpNE(ir.CreateAShr(result, rhs.value), lhs.value), "bitwise shift overflow");
                        } else result = ir.CreateAShr(lhs.value, rhs.value);
                    }
                    check_bitwise_range(result);
                    return {result, "number"};
                }
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
        auto literal = [&](const char* value) { ir.CreateCall(printf_fn, {text("%s"), text(value)}); };
        if (is_tuple(v.type)) {
            auto types = tuple_types(v.type);
            literal("(");
            for (size_t i = 0; i < types.size(); ++i) {
                if (i) literal(", ");
                print_value(tuple_item(v, types, i, loc), loc);
            }
            if (types.size() == 1) literal(",");
            literal(")");
        } else if (is_list(v.type) || is_set(v.type)) {
            auto set = is_set(v.type);
            auto type = v.type.substr(set ? 7 : 8);
            literal(set ? "set([" : "[");
            find_index(list_length(v), [&](llvm::Value* i) {
                auto sep = ir.CreateSelect(ir.CreateICmpEQ(i, ir.getInt64(0)), text(""), text(", "));
                ir.CreateCall(printf_fn, {text("%s"), sep});
                print_value(list_item(v, i, type, loc), loc);
                return ir.getFalse();
            });
            literal(set ? "])" : "]");
        } else if(v.type=="text") ir.CreateCall(printf_fn,{text("%s"),v.value});
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
                stmt.name == "decimal" || stmt.name == "bool" || stmt.name == "void" || stmt.name == "hashmap")
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
                    stmt.foreign_symbol == "realloc" || stmt.foreign_symbol == "memmove" ||
                    stmt.foreign_symbol == "calloc" || stmt.foreign_symbol == "free" ||
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
        case Stmt::Kind::Unpack:
            bind_names(stmt, unpack(expression(*stmt.expression), stmt.names.size(), stmt.location));
            break;
        case Stmt::Kind::Set: {
            if (!stmt.names.empty()) {
                bind_names(stmt, unpack(expression(*stmt.expression), stmt.names.size(), stmt.location));
                break;
            }
            if (stmt.target) {
                const auto& target = *stmt.target;
                auto collection = expression(*target.children[0]);
                if (!is_list(collection.type) && !is_hashmap(collection.type))
                    fail(target.location, "indexing requires a list, vector, or hashmap");
                auto index = expression(*target.children[1]);
                if (is_hashmap(collection.type)) {
                    auto [key_type, value_type] = hashmap_types(collection.type);
                    require_type(index, key_type, target.children[1]->location);
                    auto value = expression(*stmt.expression, value_type);
                    require_type(value, value_type, stmt.expression->location);
                    hashmap_store(collection.value, index, value);
                    break;
                }
                require_type(index, "number", target.children[1]->location);
                auto type = collection.type.substr(8);
                auto value = expression(*stmt.expression, type);
                require_type(value, type, stmt.expression->location);
                auto address = index_address(target, collection, index);
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
        case Stmt::Kind::Add:
        case Stmt::Kind::Remove: {
            auto value = expression(*stmt.expression);
            auto list = expression(*stmt.target);
            if (stmt.kind == Stmt::Kind::Remove && is_hashmap(list.type)) {
                auto key_type = hashmap_types(list.type).first;
                require_type(value, key_type, stmt.expression->location);
                auto removed = hashmaps.remove(list.value, hashmap_key(value), key_type == "text");
                raise_if(ir.CreateNot(removed), "key not found in hashmap");
                break;
            }
            if (!is_list(list.type) && !is_set(list.type)) fail(stmt.target->location, "add and remove require a list, vector, or set");
            auto type = list.type.substr(is_set(list.type) ? 7 : 8);
            require_type(value, type, stmt.expression->location);
            auto layout = list_layout();
            auto length_ptr = ir.CreateStructGEP(layout, list.value, 0);
            auto data_ptr = ir.CreateStructGEP(layout, list.value, 1);
            auto length = ir.CreateLoad(ir.getInt64Ty(), length_ptr);
            auto element_type = llvm_type(type, stmt.location);
            auto null = llvm::ConstantPointerNull::get(ir.getPtrTy());
            auto element_size = ir.CreatePtrToInt(ir.CreateGEP(element_type, null, ir.getInt64(1)), ir.getInt64Ty());
            auto fn = ir.GetInsertBlock()->getParent();
            if (stmt.kind == Stmt::Kind::Add) {
                if (is_set(list.type)) insert_set(list, value, stmt.location);
                else append_item(list, value, stmt.location);
            } else {
                auto data = ir.CreateLoad(ir.getPtrTy(), data_ptr);
                auto start = ir.GetInsertBlock();
                auto search = llvm::BasicBlock::Create(context, "list.search", fn);
                auto compare = llvm::BasicBlock::Create(context, "list.compare", fn);
                auto next = llvm::BasicBlock::Create(context, "list.next", fn);
                auto found = llvm::BasicBlock::Create(context, "list.found", fn);
                ir.CreateBr(search);
                ir.SetInsertPoint(search);
                auto index = ir.CreatePHI(ir.getInt64Ty(), 2);
                index->addIncoming(ir.getInt64(0), start);
                raise_if(ir.CreateICmpUGE(index, length), is_set(list.type) ? "item not found in set" : "item not found in list");
                ir.CreateBr(compare);
                ir.SetInsertPoint(compare);
                auto item = ir.CreateLoad(element_type, ir.CreateGEP(element_type, data, index));
                ir.CreateCondBr(equal_values({item, type}, value, stmt.location), found, next);
                ir.SetInsertPoint(next);
                auto increment = ir.CreateAdd(index, ir.getInt64(1));
                ir.CreateBr(search);
                index->addIncoming(increment, next);
                ir.SetInsertPoint(found);
                auto after = ir.CreateAdd(index, ir.getInt64(1));
                auto dest = ir.CreateGEP(element_type, data, index);
                auto src = ir.CreateGEP(element_type, data, after);
                auto bytes = ir.CreateMul(ir.CreateSub(length, after), element_size);
                ir.CreateMemMove(dest, llvm::MaybeAlign(), src, llvm::MaybeAlign(), bytes);
                ir.CreateStore(ir.CreateSub(length, ir.getInt64(1)), length_ptr);
            }
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
            if (is_tuple(list.type)) {
                auto types = tuple_types(list.type);
                if (types.empty()) break;
                auto type = types.front();
                if (!std::all_of(types.begin(), types.end(), [&](const auto& field) { return field == type; }))
                    fail(stmt.expression->location, "for each requires tuple elements of the same type");
                auto element = llvm_type(type, stmt.location);
                auto [header, data] = allocate_list(element, ir.getInt64(types.size()));
                for (size_t i = 0; i < types.size(); ++i)
                    ir.CreateStore(tuple_item(list, types, i, stmt.location).value, ir.CreateGEP(element, data, ir.getInt64(i)));
                list = {header, "list of " + type};
            }
            if (!is_list(list.type) && !is_set(list.type)) fail(stmt.expression->location, "for each requires a list or vector or set");
            auto layout = list_layout();
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
            auto length = ir.CreateLoad(ir.getInt64Ty(), ir.CreateStructGEP(layout, list.value, 0));
            ir.CreateCondBr(ir.CreateICmpULT(index, length), body, done);
            ir.SetInsertPoint(body);
            auto data = ir.CreateLoad(ir.getPtrTy(), ir.CreateStructGEP(layout, list.value, 1));
            auto type = list.type.substr(is_set(list.type) ? 7 : 8);
            auto element_type = llvm_type(type, stmt.location);
            auto value = ir.CreateLoad(element_type, ir.CreateGEP(element_type, data, index));
            auto outer = variables;
            Value item{value, type};
            bind_names(stmt, stmt.names.size() == 1 ? std::vector<Value>{item} :
                unpack(item, stmt.names.size(), stmt.location), true);
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
    explicit Generator(llvm::Module& module) : module(module), context(module.getContext()) {
        auto signature = llvm::FunctionType::get(ir.getInt32Ty(), {ir.getPtrTy()}, true);
        printf_fn = module.getOrInsertFunction("printf", signature);
        pending_error = new llvm::GlobalVariable(module, ir.getPtrTy(), false, llvm::GlobalValue::InternalLinkage, llvm::ConstantPointerNull::get(ir.getPtrTy()), "tell.error");
        input_stream = new llvm::GlobalVariable(module, ir.getPtrTy(), false, llvm::GlobalValue::InternalLinkage, llvm::ConstantPointerNull::get(ir.getPtrTy()), "tell.input");
    }

    void generate(const Program& program) {
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
        if (llvm::verifyModule(module, &llvm::errs()))
            throw std::runtime_error("generated invalid LLVM IR");
    }
};
} // namespace

std::unique_ptr<llvm::Module> generate_module(const Program& program, llvm::LLVMContext& context) {
    auto module = std::make_unique<llvm::Module>("tell", context);
    Generator(*module).generate(program);
    return module;
}

#ifdef TELL_EMBEDDED_CODEGEN
void generate_object(const Program& program, llvm::SmallVectorImpl<char>& object, const std::string& target_triple) {
    llvm::LLVMContext context;
    auto module = generate_module(program, context);
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
    module->setTargetTriple(triple);
    module->setDataLayout(machine->createDataLayout());
    llvm::raw_svector_ostream out(object);
    llvm::legacy::PassManager passes;
    if (machine->addPassesToEmitFile(passes, out, nullptr, llvm::CodeGenFileType::ObjectFile))
        throw std::runtime_error("LLVM cannot emit object code for this target");
    passes.run(*module);
}
#endif
void check_program(const Program& program) {
    llvm::LLVMContext context;
    generate_module(program, context);
}
void generate_ir(const Program& program, const std::string& path) {
    llvm::LLVMContext context;
    auto module = generate_module(program, context);
    std::error_code error;
    llvm::raw_fd_ostream out(path, error);
    if (error) throw std::runtime_error("cannot write temporary LLVM IR: " + error.message());
    module->print(out, nullptr);
    out.flush();
    if (out.has_error()) throw std::runtime_error("cannot write temporary LLVM IR");
}
} // namespace tell
