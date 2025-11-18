package main

import (
	"fmt"

	"mibk.dev/phpfmt/phpdoc/phptype"
	"mibk.dev/phpfmt/token"
)

type File struct {
	Path  string
	Scope *scope
}

type scope struct {
	Open, close token.Type
	Params      []Param
	Stmts       []*Stmt
}

type Param struct {
	Name  string
	Class string
}

type Debug struct {
	Var string
	Pos token.Pos
}

type Stmt struct {
	Nodes []any
}

type typeDecl interface {
	addMember(m *Member) error
}

type Class struct {
	Name    string
	Extends string // or empty
	Traits  []string
	Members map[string]*Member
}

func (c *Class) addMember(m *Member) error {
	if _, ok := c.Members[m.Name]; ok {
		return fmt.Errorf("class %s already has %s", c.Name, m.Name)
	}
	c.Members[m.Name] = m
	return nil
}

type Trait struct {
	Name    string
	Members map[string]*Member
}

func (t *Trait) addMember(m *Member) error {
	if _, ok := t.Members[m.Name]; ok {
		return fmt.Errorf("trait %s already has %s", t.Name, m.Name)
	}
	t.Members[m.Name] = m
	return nil
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
