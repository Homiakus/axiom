# Draft EBNF

This grammar is intentionally small. It defines the TRIZ surface syntax, not the
current Axiom v0 parser.

```ebnf
File        = SystemDecl, { Decl } ;
Decl        = StateDecl | EventDecl | ConditionDecl | ProfileDecl
            | FunctionDecl | RuleDecl | AlwaysDecl | ViewDecl ;

SystemDecl  = "system", Identifier, Newline ;

StateDecl   = "state", TypeName, ":", Newline,
              Indent, FieldDecl, { FieldDecl }, Dedent ;

FieldDecl   = Identifier, ":", TypeRef, [ "=", Expr ], Newline ;

EventDecl   = "event", Identifier,
              [ "(", [ ParamDecl, { ",", ParamDecl } ], ")" ],
              Newline ;

ParamDecl   = Identifier, ":", TypeRef ;

ConditionDecl = "condition", Identifier, ":", Newline,
                Indent, Expr, { Newline, Expr }, Dedent ;

ProfileDecl = "profile", Identifier, ":", Newline,
              Indent, ProfileLine, { ProfileLine }, Dedent ;

FunctionDecl = "function", Identifier,
               "(", [ ParamDecl, { ",", ParamDecl } ], ")",
               "->", ObjectType, Newline ;

RuleDecl    = "rule", Identifier, "when", ":", Newline,
              Indent, Expr, { Newline, Expr }, Dedent,
              [ DoBlock ],
              ThenBlock ;

DoBlock     = "do", [ Identifier ], ":", Newline,
              Indent, Identifier, "=", FunctionCall, Newline, Dedent ;

ThenBlock   = "then", ":", Newline,
              Indent, WriteLine, { WriteLine }, Dedent ;

WriteLine   = "set", Path, "=", Expr, Newline ;

AlwaysDecl  = "always", Identifier, ":", Newline,
              Indent, Expr, { Newline, Expr }, Dedent ;

ViewDecl    = "view", Identifier, ":", Newline,
              Indent, Binding, { Binding }, Dedent ;

Binding     = Identifier, "=", Expr, Newline ;

FunctionCall = Identifier, "(", [ Expr, { ",", Expr } ], ")" ;

Expr        = OrExpr ;
OrExpr      = AndExpr, { "or", AndExpr } ;
AndExpr     = CompareExpr, { "and", CompareExpr } ;
CompareExpr = UnaryExpr,
              [ ( "==" | "!=" | ">" | ">=" | "<" | "<="
                | "in" | "implies" ), UnaryExpr ] ;
UnaryExpr   = [ "not" ], Primary ;
Primary     = Literal | Path | FunctionLike | "(", Expr, ")" ;
```

Open points for implementation:

- exact collection/index syntax for `Zone[event.zone]`;
- whether `condition` can reference `event.*`;
- syntax for named arguments in function calls;
- metadata/version block.
