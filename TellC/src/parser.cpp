#include "tell/parser.hpp"
#include <cctype>
#include <utility>

namespace tell {
namespace {
struct Token { std::string text; Location loc; enum Kind { Word, Number, String, Symbol, End } kind; };
std::string lower(std::string s) { for (char& c : s) c=static_cast<char>(std::tolower(static_cast<unsigned char>(c))); return s; }
std::vector<Token> lex(const std::string& s) {
    std::vector<Token> v; size_t i=0, line=1, col=1;
    auto add=[&](std::string t, Location l, Token::Kind k){v.push_back({std::move(t),l,k});};
    while(i<s.size()) {
        unsigned char c=s[i]; Location l{line,col};
        if(c=='\r') { ++i; continue; }
        if(c=='\n') { add("\n",l,Token::Symbol); ++i; ++line; col=1; continue; }
        if(c==' '||c=='\t') { ++i; ++col; continue; }
        if(c=='#') { while(i<s.size()&&s[i]!='\n') {++i;++col;} continue; }
        size_t start=i;
        if(std::isalpha(c)||c=='_'||c>=128) { while(i<s.size()) { unsigned char d=s[i]; if(!(std::isalnum(d)||d=='_'||d>=128)) break; ++i; ++col; } add(s.substr(start,i-start),l,Token::Word); continue; }
        if(std::isdigit(c)) { while(i<s.size()&&std::isdigit((unsigned char)s[i])) {++i;++col;} if(i+1<s.size()&&s[i]=='.'&&std::isdigit((unsigned char)s[i+1])) {++i;++col; while(i<s.size()&&std::isdigit((unsigned char)s[i])) {++i;++col;} } add(s.substr(start,i-start),l,Token::Number); continue; }
        if(c=='\''||c=='"') { char quote=c; ++i;++col; std::string value; bool closed=false; while(i<s.size()&&s[i]!='\n') { char d=s[i++];++col; if(d==quote){closed=true;break;} if(d=='\\') {if(i==s.size()||s[i]=='\n') break; char e=s[i++];++col; if(e=='n') value+='\n'; else if(e=='t') value+='\t'; else if(e=='r') value+='\r'; else if(e=='\\'||e=='\''||e=='"') value+=e; else throw Error(l,"unsupported string escape");} else value+=d; } if(!closed) throw Error(l,"unterminated string"); add(value,l,Token::String); continue; }
        ++i; ++col; std::string op(1,c); if(i<s.size() && ((c=='='&&s[i]=='=')||(c=='!'&&s[i]=='=')||(c=='<'&&s[i]=='=')||(c=='>'&&s[i]=='=')||(c=='*'&&s[i]=='*')||(c=='/'&&s[i]=='/')||(c=='&'&&s[i]=='&')||(c=='|'&&s[i]=='|')||(c=='<'&&s[i]=='<')||(c=='>'&&s[i]=='>'))) {op+=s[i++];++col;} add(op,l,Token::Symbol);
    }
    v.push_back({"",{line,col},Token::End}); return v;
}
class Parser {
    std::vector<Token> t; size_t p=0;
    const Token& peek() const {return t[p];}
    bool is(const std::string& s) const {return (peek().kind==Token::Word || peek().kind==Token::Symbol) && lower(peek().text)==s;}
    bool take(const std::string& s) {if(is(s)){++p;return true;}return false;}
    Token get(){return t[p++];}
    void expect(const std::string& s){if(!take(s)) throw Error(peek().loc,"expected '"+s+"'");}
    std::string name(){if(peek().kind!=Token::Word) throw Error(peek().loc,"expected identifier"); return get().text;}
    std::string type_name() {
        auto value = name();
        auto canonical = lower(value);
        if (canonical == "list" || canonical == "vector") {
            expect("of");
            auto element = type_name();
            if (element == "void") throw Error(peek().loc, "collection element type cannot be void");
            return "list of " + element;
        }
        if (canonical == "hashmap") {
            expect("of");
            auto key_location = peek().loc;
            auto key = lower(name());
            if (key != "text" && key != "number" && key != "bool")
                throw Error(key_location, "hashmap key type must be text, number, or bool");
            expect("to");
            auto element_location = peek().loc;
            auto element = type_name();
            if (element == "void") throw Error(element_location, "hashmap value type cannot be void");
            return "hashmap of " + key + " to " + element;
        }
        if (canonical == "text" || canonical == "number" || canonical == "decimal" ||
            canonical == "bool" || canonical == "void") return canonical;
        return value;
    }
    // A phrase is a name only when followed by a call. Operator words still
    // delimit expressions, for example in "value and ready()".
    std::string call_name(std::string value) {
        size_t end = p;
        while (t[end].kind == Token::Word && !prec(lower(t[end].text))) ++end;
        if (t[end].kind == Token::Symbol && t[end].text == "(") {
            while (p < end) value += " " + name();
        }
        return value;
    }
    void separators(){while(take("\n")||take(";")) {}}
    void end(){if(peek().kind==Token::End) return; if(!is("\n")&&!is(";")) throw Error(peek().loc,"expected newline or ';' after statement"); separators();}
    std::unique_ptr<Expr> node(Expr::Kind k, Token a) { auto e=std::make_unique<Expr>(); e->kind=k;e->location=a.loc;e->value=a.text;return e; }
    int prec(const std::string& s){if(s=="or"||s=="||")return 1;if(s=="and"||s=="&&")return 2;if(s=="is"||s=="equal"||s=="="||s=="=="||s=="!="||s=="<"||s==">"||s=="<="||s==">="||s=="in")return 3;if(s=="|")return 4;if(s=="^")return 5;if(s=="&")return 6;if(s=="<<"||s==">>")return 7;if(s=="+"||s=="-"||s=="plus"||s=="minus")return 8;if(s=="*"||s=="/"||s=="//"||s=="%"||s=="times")return 9;if(s=="**")return 11;return 0;}
    std::unique_ptr<Expr> expr(int min=1){
        Token a=get(); std::unique_ptr<Expr> x;
        auto w=lower(a.text);
        if(a.kind==Token::Number) x=node(Expr::Kind::Number,a);
        else if(a.kind==Token::String) x=node(Expr::Kind::String,a);
        else if(w=="true"||w=="false") x=node(Expr::Kind::Boolean,a);
        else if(w=="nothing"||w=="null"||w=="nil") x=node(Expr::Kind::Nothing,a);
        else if(w=="not"||w=="!"||w=="~"||w=="-"||w=="+") {x=node(Expr::Kind::Unary,a);x->children.push_back(expr(10));}
        else if(a.text=="("){x=expr();expect(")");}
        else if(a.text=="["){x=node(Expr::Kind::List,a);if(!take("]")){do{x->children.push_back(expr());}while(take(","));expect("]");}}
        else if (a.kind == Token::Symbol && a.text == "{") {
            x = node(Expr::Kind::Hashmap, a);
            auto skip_newlines = [&] { while (take("\n")) {} };
            skip_newlines();
            while (!is("}")) {
                x->children.push_back(expr());
                expect(":");
                skip_newlines();
                x->children.push_back(expr());
                skip_newlines();
                if (!take(",")) break;
                skip_newlines();
            }
            expect("}");
        }
        else if(a.kind==Token::Word) { a.text=call_name(a.text); x=node(Expr::Kind::Name,a); }
        else throw Error(a.loc,"expected expression");
        for(;;){
            if(is("(")){Token op=get();auto n=node(Expr::Kind::Call,op);n->children.push_back(std::move(x));if(!take(")")){do{n->children.push_back(expr());}while(take(","));expect(")");}x=std::move(n);continue;}
            if(is("[")){Token op=get();auto n=node(Expr::Kind::Index,op);n->children.push_back(std::move(x));n->children.push_back(expr());expect("]");x=std::move(n);continue;}
            if(is(".") && p+1<t.size() && t[p+1].kind==Token::Word){Token op=get();op.text=call_name(name());auto n=node(Expr::Kind::Member,op);n->children.push_back(std::move(x));x=std::move(n);continue;}
            if(is("times") && p+1<t.size() && t[p+1].text==":") break;
            int bp=prec(lower(peek().text));if(bp<min||bp==0)break;Token op=get();
            auto word=lower(op.text);
            if(word=="is") { if(take("not")){expect("equal");expect("to");op.text="!=";} else {if(take("equal")) expect("to");op.text="==";} }
            else if(word=="equal") {expect("to");op.text="==";}
            else if(word=="plus") op.text="+"; else if(word=="minus") op.text="-"; else if(word=="times") op.text="*";
            auto n=node(Expr::Kind::Binary,op);n->children.push_back(std::move(x));n->children.push_back(expr(bp+(op.text=="**"?0:1)));x=std::move(n);
        } return x;
    }
    std::vector<Stmt> body(const std::string& until, bool branches=false){std::vector<Stmt> out;separators();while(peek().kind!=Token::End&&!is("end")&&!(branches&&(is("otherwise")||is("else")))) out.push_back(statement()); if(peek().kind==Token::End) throw Error(peek().loc,"expected end "+until); return out;}
    Stmt statement(){Token first=peek();Stmt s{};s.location=first.loc;
        if(take("extern")){
            s.kind=Stmt::Kind::ForeignFunction;
            s.foreign_language=lower(name());
            if(s.foreign_language!="c" && s.foreign_language!="cpp" && s.foreign_language!="swift") throw Error(first.loc,"expected foreign language c, cpp, or swift");
            expect("function"); s.name=name(); expect("(");
            auto foreign_type=[&](bool allow_void) { Token tok=peek(); auto type=lower(name());
                if(type!="int32" && type!="int64" && type!="float32" && type!="float64" && !(allow_void && type=="void"))
                    throw Error(tok.loc,"expected C ABI type int32, int64, float32, float64" + std::string(allow_void ? ", or void" : ""));
                return type;
            };
            if(!take(")")){ do { ForeignParameter param; param.location=peek().loc; param.type=foreign_type(false); param.name=name(); s.foreign_parameters.push_back(std::move(param)); } while(take(",")); expect(")"); }
            expect("returns"); s.type=foreign_type(true); s.foreign_symbol=s.name;
            if(take("as")){if(peek().kind!=Token::String) throw Error(peek().loc,"expected foreign symbol string"); s.foreign_symbol=get().text; if(s.foreign_symbol.empty()) throw Error(first.loc,"foreign symbol must not be empty");}
            end();return s;
        }
        if(take("let")){s.kind=Stmt::Kind::Let;s.constant=take("constant");s.type=type_name();if(s.type=="void")throw Error(first.loc,"variable type cannot be void");s.instance_member=take("self");s.name=name();expect("be");s.expression=expr();end();return s;}
        if (take("set")) {
            s.kind = Stmt::Kind::Set;
            auto target = peek();
            s.name = name();
            if (is("[") || is(".")) {
                s.target = node(Expr::Kind::Name, target);
                while (is("[") || is(".")) {
                    auto selector = get();
                    auto access = node(selector.text == "[" ? Expr::Kind::Index : Expr::Kind::Member, selector);
                    access->children.push_back(std::move(s.target));
                    if (access->kind == Expr::Kind::Index) {
                        access->children.push_back(expr());
                        expect("]");
                    } else {
                        access->value = name();
                    }
                    s.target = std::move(access);
                }
                if (s.target->kind != Expr::Kind::Index)
                    throw Error(s.target->location, "set requires a variable or indexed collection element");
            }
            expect("to");
            s.expression = expr();
            end();
            return s;
        }
        if(take("add") || take("remove")){s.kind=lower(first.text)=="add"?Stmt::Kind::Add:Stmt::Kind::Remove;s.expression=expr();expect(s.kind==Stmt::Kind::Add?"to":"from");s.target=expr();end();return s;}
        if(take("say")||take("show")||take("print")){s.kind=Stmt::Kind::Say;s.expression=expr();end();return s;}
        if(take("return")||take("give")){s.kind=Stmt::Kind::Return;if(lower(first.text)=="give")expect("back");if(!is("\n")&&!is(";")&&peek().kind!=Token::End)s.expression=expr();end();return s;}
        if(take("raise")){expect("an");expect("error");expect("saying");s.kind=Stmt::Kind::Raise;s.expression=expr();end();return s;}
        if(take("try")){s.kind=Stmt::Kind::Try;expect(":");end();separators();
            while(peek().kind!=Token::End && !is("end") && !(is("if") && p+1<t.size() && lower(t[p+1].text)=="something")) s.body.push_back(statement());
            if(is("if")){expect("if");expect("something");expect("goes");expect("wrong");expect(":");end();Stmt handler{};handler.kind=Stmt::Kind::Else;handler.location=first.loc;handler.body=body("try");s.branches.push_back(std::move(handler));}
            expect("end");expect("try");take(".");end();return s;}
        if(take("switch")){
            s.kind=Stmt::Kind::Switch;s.expression=expr();expect(":");end();
            size_t case_column=peek().loc.column;
            if(peek().kind==Token::End || case_column<=first.loc.column || !is("if"))
                throw Error(peek().loc,"expected an indented switch case");
            while(peek().kind!=Token::End && peek().loc.column>first.loc.column){
                Token start=peek();
                if(start.loc.column!=case_column || !take("if")) throw Error(start.loc,"expected switch case at the same indentation");
                expect("it");expect("is");Stmt arm{};arm.kind=Stmt::Kind::Case;arm.location=start.loc;arm.expression=expr();expect(":");
                if(peek().kind!=Token::End && !is("\n") && !is(";")) arm.body.push_back(statement());
                else { end(); while(peek().kind!=Token::End && peek().loc.column>case_column) arm.body.push_back(statement()); }
                if(arm.body.empty()) throw Error(start.loc,"switch case requires a body");
                s.branches.push_back(std::move(arm));
            }
            if(is("end") && peek().loc.column==first.loc.column && p+1<t.size() && lower(t[p+1].text)=="switch") {get();get();take(".");end();}
            return s;
        }
        if(take("if")){s.kind=Stmt::Kind::If;s.expression=expr();expect(":");end();s.body=body("if",true);while(is("otherwise")||is("else")){Token b=get();Stmt branch{};branch.location=b.loc;if(take("if")){branch.kind=Stmt::Kind::ElseIf;branch.expression=expr();}else branch.kind=Stmt::Kind::Else;expect(":");end();branch.body=body("if",true);s.branches.push_back(std::move(branch));if(s.branches.back().kind==Stmt::Kind::Else)break;}expect("end");expect("if");take(".");end();return s;}
        if(take("for")){expect("each");s.kind=Stmt::Kind::For;s.name=name();expect("in");s.expression=expr();expect(":");end();s.body=body("for");expect("end");expect("for");take(".");end();return s;}
        if(take("break")){s.kind=Stmt::Kind::Break;end();return s;}
        if(take("continue")){s.kind=Stmt::Kind::Continue;end();return s;}
        if(take("repeat")){s.kind=Stmt::Kind::Repeat;s.expression=expr();expect("times");expect(":");end();s.body=body("repeat");expect("end");expect("repeat");take(".");end();return s;}
        if(take("while")){s.kind=Stmt::Kind::While;s.expression=expr();expect(":");end();s.body=body("while");expect("end");expect("while");take(".");end();return s;}
        if(take("object")){
            s.kind=Stmt::Kind::Object;s.name=name();expect(":");end();
            bool own_seen=false, others_seen=false;
            while(!is("end")){
                if(peek().kind==Token::End) throw Error(peek().loc,"expected end object");
                Token section_token=peek(); Stmt section{};section.kind=Stmt::Kind::AccessSection;section.location=section_token.loc;
                if(take("own")){if(own_seen)throw Error(section_token.loc,"duplicate own section");own_seen=true;section.name="own";}
                else if(take("others")){if(others_seen)throw Error(section_token.loc,"duplicate others section");others_seen=true;section.name="others";}
                else throw Error(peek().loc,"expected own or others section");
                expect(":");end();
                while(!is("own")&&!is("others")&&!is("end")&&peek().kind!=Token::End){
                    auto member=statement();if(member.kind!=Stmt::Kind::Let && member.kind!=Stmt::Kind::Function) throw Error(member.location,"object sections only allow fields and methods");section.body.push_back(std::move(member));
                }
                s.body.push_back(std::move(section));
            }
            expect("end");expect("object");take(".");end();return s;
        }
        if(take("to")){
            s.kind=Stmt::Kind::Function;s.name=name();
            while(peek().kind==Token::Word && !is("with") && !is("out")) { s.name+=" "+name(); }
            if(take("(")){
                if(!take(")")){do{Parameter param;param.location=peek().loc;param.type=type_name();if(param.type=="void")throw Error(param.location,"parameter type cannot be void");expect(":");param.name=name();s.names.push_back(param.name);s.parameters.push_back(std::move(param));}while(take(","));expect(")");}
            } else if(take("with")){do{s.names.push_back(name());}while(take("and")||take(","));}
            if(take("out")) s.return_type=type_name();
            expect(":");end();s.body=body("function");expect("end");expect("function");take(".");end();return s;
        }
        s.kind=Stmt::Kind::Expression;s.expression=expr();end();return s;
    }
public: explicit Parser(const std::string& source):t(lex(source)){} Program parse(){Program result;separators();while(peek().kind!=Token::End) result.statements.push_back(statement());return result;}
};
}
Program parse(const std::string& source){return Parser(source).parse();}
}
