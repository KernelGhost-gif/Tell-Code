#include "tell/parser.hpp"
#include <string>
int main() {
    auto p = tell::parse("extern c function add(int32 a, int32 b) returns int32 as \"c_add\"; extern swift function twice(float64 x) returns float64\n");
    if (p.statements.size()!=2) return 1;
    const auto& c=p.statements[0]; const auto& swift=p.statements[1];
    if (c.kind!=tell::Stmt::Kind::ForeignFunction || c.foreign_language!="c" || c.name!="add" || c.foreign_symbol!="c_add" || c.type!="int32" || c.foreign_parameters.size()!=2 || c.foreign_parameters[0].type!="int32" || c.foreign_parameters[1].name!="b") return 1;
    if (swift.foreign_language!="swift" || swift.foreign_symbol!="twice" || swift.type!="float64") return 1;
    try { tell::parse("extern c function bad(void x) returns void\n"); return 1; } catch (const tell::Error&) {}
    try { tell::parse("extern rust function bad() returns void\n"); return 1; } catch (const tell::Error&) {}
}
