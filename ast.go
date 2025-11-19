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
	addProperty(m *Property) error
	addMethod(m *Function) error
}

type Class struct {
	Name       string
	Extends    string // or empty
	Traits     []string
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
	Name       string
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
	Name  string
	Type  phptype.Type
	Class string
}

type Function struct {
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
