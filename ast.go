package main

import (
	"mibk.dev/phpfmt/phpdoc/phptype"
	"mibk.dev/phpfmt/token"
)

type File struct {
	Scope *scope
}

type scope struct {
	Open, close token.Type
	Nodes       []*stmt
}

type stmt struct {
	Nodes []any
}

type Class struct {
	Name    string
	Extends string // or empty
	Members map[string]*Member
}

type Member struct {
	Name string
	Type phptype.Type
}

type Expr interface{}

type VarExpr struct {
	Name string
}

type MemberAccess struct {
	Rcvr Expr
	Name string
}
