#include "tell/parser.hpp"
#include <iostream>
#include <string>

int main() {
    const std::string source = R"(to factorial with n:
    if n <= 1:
        return 1
    end if
    return n * factorial(n - 1)
end function
let number result be factorial(5)
)";
    auto program = tell::parse(source);
    using S = tell::Stmt;
    using E = tell::Expr;
    if (program.statements.size() != 2) return 1;
    const auto& fn = program.statements[0];
    if (fn.kind != S::Kind::Function || fn.name != "factorial" || fn.names.size() != 1 || fn.names[0] != "n" || fn.body.size() != 2) return 1;
    if (fn.body[0].kind != S::Kind::If || fn.body[0].body.size() != 1 || fn.body[0].body[0].kind != S::Kind::Return) return 1;
    const auto& ret = fn.body[1];
    if (ret.kind != S::Kind::Return || !ret.expression || ret.expression->kind != E::Kind::Binary || ret.expression->children.size() != 2) return 1;
    const auto& call = *ret.expression->children[1];
    if (call.kind != E::Kind::Call || call.children.size() != 2 || call.children[0]->kind != E::Kind::Name || call.children[0]->value != fn.name || call.children[1]->kind != E::Kind::Binary) return 1;
    if (program.statements[1].expression->kind != E::Kind::Call) return 1;
    std::cout << "Recursive function AST parsed successfully\n";
}
