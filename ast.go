package main

import (
	"mibk.dev/phpfmt/phpdoc/phptype"
	"mibk.dev/phpfmt/token"
)

type File struct {
	Path  string
	Scope *scope
}

type scope struct {
	Open, close token.Type
	Stmts       []*Stmt
}

type Debug struct {
	Var string
	Pos token.Pos
}

type Stmt struct {
	Nodes []any
}

type Class struct {
	Name    string
	Extends string // or empty
	Members map[string]*Member
}

type Member struct {
	Name  string
	Type  phptype.Type
	Class string
}

type Expr interface{}

type NewInstance struct {
	Class string
}

type VarExpr struct {
	Name string
}

type MemberAccess struct {
	Rcvr Expr
	Name string
	Pos  token.Pos
}

type AssignExpr struct {
	Left  Expr
	Right Expr
}
