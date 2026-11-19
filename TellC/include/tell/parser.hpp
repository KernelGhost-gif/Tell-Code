#pragma once
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

namespace tell {
struct Location { size_t line = 1, column = 1; };
struct Error : std::runtime_error {
    Location location;
    Error(Location loc, const std::string& message) : std::runtime_error(message), location(loc) {}
};
struct Expr {
    enum class Kind { Number, String, Name, Boolean, Nothing, Unary, Binary, Call, Member, Index, List, Hashmap };
    Kind kind;
    Location location;
    std::string value;
    std::vector<std::unique_ptr<Expr>> children;
};
struct Parameter { std::string type; std::string name; Location location; };
struct ForeignParameter { std::string type; std::string name; Location location; };
struct Stmt {
    enum class Kind { Let, Set, Add, Remove, Say, Check, Return, Expression, If, ElseIf, Else, Switch, Case, While, Repeat, For, Function, Object, AccessSection, ForeignFunction, Break, Continue, Try, Raise };
    Kind kind;
    Location location;
    std::string name;
    std::vector<std::string> names;
    std::vector<Parameter> parameters;
    std::string return_type;
    bool instance_member = false;
    std::string foreign_language;
    std::string foreign_symbol;
    std::vector<ForeignParameter> foreign_parameters;
    bool constant = false;
    std::string type;
    std::unique_ptr<Expr> expression;
    std::unique_ptr<Expr> target;
    std::vector<Stmt> body;
    std::vector<Stmt> branches;
};
struct Program { std::vector<Stmt> statements; };
Program parse(const std::string& source);
}
