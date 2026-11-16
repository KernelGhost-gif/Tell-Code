#include "codegen.hpp"
#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/Module.h>
#include <llvm/IR/Verifier.h>
#include <llvm/Support/raw_ostream.h>
#include <algorithm>
#include <cctype>
#include <map>
#include <set>

namespace tell {
namespace {
struct Value { llvm::Value* value; std::string type; };
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
    llvm::IRBuilder<> ir{module};
    std::map<std::string, Value> variables;
    llvm::FunctionCallee printf_fn;
    std::map<std::string, Object> objects;
    std::map<std::string, Routine> functions;
    Value receiver{nullptr, ""};
    const Routine* current_routine = nullptr;

    llvm::Type* llvm_type(const std::string& type, Location loc) {
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
        if (local != variables.end()) return local->second;
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
                    return {ir.CreateCall(object->second.constructor), callee.value};
                }
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
            auto value = expression(argument);
            require_type(value, parameters[i].type, argument.location);
            args.push_back(value.value);
        }
        return {ir.CreateCall(routine->function, args), routine->syntax->return_type};
    }

    [[noreturn]] void fail(tell::Location loc, const std::string& message) { throw tell::Error(loc, message); }
    llvm::Value* text(const std::string& s) { return ir.CreateGlobalString(s); }
    Value expression(const tell::Expr& e) {
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
        if (e.kind == K::Name) return lookup(e.value, e.location);
        if (e.kind == K::Member) return field(expression(*e.children[0]), e.value, e.location);
        if (e.kind == K::Call) return call(e);
        if (e.kind == K::Binary) {
            auto lhs = expression(*e.children[0]);
            auto rhs = expression(*e.children[1]);
            require_type(rhs, lhs.type, e.children[1]->location);
            if (e.value == "=" || e.value == "==" || e.value == "!=") {
                llvm::Value* equal;
                if (lhs.type == "void") fail(e.location, "cannot compare void values");
                if (lhs.type == "text") {
                    auto signature = llvm::FunctionType::get(ir.getInt32Ty(), {ir.getPtrTy(), ir.getPtrTy()}, false);
                    auto strcmp_fn = module.getOrInsertFunction("strcmp", signature);
                    equal = ir.CreateICmpEQ(ir.CreateCall(strcmp_fn, {lhs.value, rhs.value}), ir.getInt32(0));
                } else if (lhs.type == "decimal") {
                    equal = ir.CreateFCmpOEQ(lhs.value, rhs.value);
                } else {
                    equal = ir.CreateICmpEQ(lhs.value, rhs.value);
                }
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
                if (objects.count(stmt.name) || functions.count(stmt.name))
                    fail(stmt.location, "duplicate callable '" + stmt.name + "'");
                functions.emplace(stmt.name, declare_routine(stmt, "", false));
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
                auto value = expression(*member.expression);
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
            auto value = expression(*stmt.expression);
            require_type(value, stmt.type, stmt.expression->location);
            variables.emplace(stmt.name, value);
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
                auto value = expression(*stmt.expression);
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
        for (const auto& parameter : routine.syntax->parameters)
            variables.emplace(parameter.name, Value{&*arg++, parameter.type});
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
    }

    void generate(const Program& program, const std::string& path) {
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
            if (stmt.kind != Stmt::Kind::Object && stmt.kind != Stmt::Kind::Function)
                emit_statement(stmt);
        }
        ir.CreateRet(ir.getInt32(0));
        if(llvm::verifyModule(module,&llvm::errs())) throw std::runtime_error("generated invalid LLVM IR");
        std::error_code ec;llvm::raw_fd_ostream out(path,ec);if(ec) throw std::runtime_error("cannot write temporary LLVM IR: "+ec.message());module.print(out,nullptr);out.flush();if(out.has_error()) throw std::runtime_error("cannot write temporary LLVM IR");
    }
};
} // namespace

void generate_ir(const Program& program, const std::string& path) {
    Generator{}.generate(program, path);
}
} // namespace tell
