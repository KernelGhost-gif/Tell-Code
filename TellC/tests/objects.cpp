#include "tell/parser.hpp"
#include <string>
int main() {
    auto p=tell::parse(R"(object MacBook:
 own:
  let text self password be "12345678"
 others:
  to check password(text: Password) out bool:
   if Password = password:
    give back true
   end if
   give back false
  end function
end object
)");
    using S=tell::Stmt; using E=tell::Expr;
    if(p.statements.size()!=1) return 1;
    auto& obj=p.statements[0];
    if(obj.kind!=S::Kind::Object || obj.name!="MacBook" || obj.body.size()!=2 || obj.body[0].name!="own" || obj.body[1].name!="others")return 1;
    auto& field=obj.body[0].body[0]; if(field.kind!=S::Kind::Let || !field.instance_member || field.type!="text" || field.name!="password")return 1;
    auto& method=obj.body[1].body[0]; if(method.kind!=S::Kind::Function || method.name!="check password" || method.return_type!="bool" || method.parameters.size()!=1 || method.parameters[0].type!="text" || method.parameters[0].name!="Password")return 1;
    auto& condition=method.body[0]; if(condition.kind!=S::Kind::If || condition.expression->kind!=E::Kind::Binary || condition.expression->value!="=")return 1;
    try {tell::parse("object X:\n own:\n let text self x be \"hi\"\n own:\n end object\n"); return 1;} catch(const tell::Error&) {}
    try {tell::parse("to f(void: x) out bool:\n return true\nend function\n"); return 1;} catch(const tell::Error&) {}
}
