package main

import (
	"fmt"

	"mibk.dev/phpfmt/token"
)

type File struct {
	Path  string
	Block *Block
}

type UseStmt struct {
	Namespace Ident
	Alias     string
}

type Block struct {
	Params []*Param
	Stmts  []*Stmt
}

type Param struct {
	Name string
	Type Ident
}

type Debug struct {
	Var string
	Pos token.Pos
}

type Stmt struct {
	Nodes []any
}

type typeDecl interface {
	addProperty(m *Property) error
	addMethod(m *Function) error
}

type Class struct {
	Name       Ident
	Extends    Ident // or empty
	Traits     []Ident
	Properties map[string]*Property
	Methods    map[string]*Function
}

func (c *Class) addProperty(p *Property) error {
	if _, ok := c.Properties[p.Name]; ok {
		return fmt.Errorf("class %s already has property %s", c.Name, p.Name)
	}
	c.Properties[p.Name] = p
	return nil
}

func (c *Class) addMethod(m *Function) error {
	if _, ok := c.Methods[m.Name]; ok {
		return fmt.Errorf("class %s already has method %s", c.Name, m.Name)
	}
	c.Methods[m.Name] = m
	return nil
}

type Trait struct {
	Name       Ident
	Properties map[string]*Property
	Methods    map[string]*Function
}

func (t *Trait) addProperty(m *Property) error {
	if _, ok := t.Properties[m.Name]; ok {
		return fmt.Errorf("trait %s already has %s", t.Name, m.Name)
	}
	t.Properties[m.Name] = m
	return nil
}

func (t *Trait) addMethod(m *Function) error {
	if _, ok := t.Methods[m.Name]; ok {
		return fmt.Errorf("class %s already has method %s", t.Name, m.Name)
	}
	t.Methods[m.Name] = m
	return nil
}

type Property struct {
	Name string
	Type Ident
}

type Function struct {
	Name    string
	Returns Ident
}

type Foreach struct {
	X     Expr
	Value Param
}

type Expr interface {
	Pos() token.Pos
}

type NewInstance struct {
	New   token.Pos
	Class Ident
}

func (e *NewInstance) Pos() token.Pos { return e.New }

type VarExpr struct {
	Dollar token.Pos
	Name   string
}

func (e *VarExpr) Pos() token.Pos { return e.Dollar }

type IndexExpr struct {
	X Expr
}

func (e *IndexExpr) Pos() token.Pos { return e.X.Pos() }

type MemberAccess struct {
	Rcvr       Expr
	NamePos    token.Pos
	Name       string
	MethodCall bool
}

func (e *MemberAccess) Pos() token.Pos { return e.NamePos }

type AssignExpr struct {
	Left  Expr
	Right Expr
}

func (e *AssignExpr) Pos() token.Pos { return e.Left.Pos() }
