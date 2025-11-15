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

type Expr interface {
	Pos() token.Pos
}

type NewInstance struct {
	New   token.Pos
	Class string
}

func (e *NewInstance) Pos() token.Pos { return e.New }

type VarExpr struct {
	Dollar token.Pos
	Name   string
}

func (e *VarExpr) Pos() token.Pos { return e.Dollar }

type MemberAccess struct {
	Rcvr    Expr
	NamePos token.Pos
	Name    string
}

func (e *MemberAccess) Pos() token.Pos { return e.NamePos }

type AssignExpr struct {
	Left  Expr
	Right Expr
}

func (e *AssignExpr) Pos() token.Pos { return e.Left.Pos() }
