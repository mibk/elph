package main

import (
	"fmt"

	"mibk.dev/phpfmt/token"
)

const TemplateParam = "T"

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

type Class struct {
	Name       Ident
	Template   Ident // or empty
	Extends    Ident // or empty
	Traits     []Ident
	Properties map[string]*Property
	Methods    map[string]*Function
}

type Trait struct {
	Name       Ident
	Properties map[string]*Property
	Methods    map[string]*Function
}

type Property struct {
	Pos    token.Pos
	Name   string
	Type   Ident
	Static bool
}

type Function struct {
	Pos     token.Pos
	Name    string
	Returns Ident
	Static  bool
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
	Class any
}

func (e *NewInstance) Pos() token.Pos { return e.New }

// TODO: Do the names make sense?

type ValueExpr struct {
	V    token.Pos
	Type Ident
}

func (e *ValueExpr) Pos() token.Pos { return e.V }

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
	Static     bool
}

func (e *MemberAccess) Pos() token.Pos { return e.Rcvr.Pos() }

type AssignExpr struct {
	Left  Expr
	Right Expr
}

func (e *AssignExpr) Pos() token.Pos { return e.Left.Pos() }

type AssertExpr struct {
	Fn   token.Pos
	Var  string
	Type Ident
}

func (e *AssertExpr) Pos() token.Pos { return e.Fn }

////////////

type typeDecl interface {
	addProperty(m *Property) error
	addMethod(m *Function) error
}

func (c *Class) addProperty(p *Property) error {
	initMap(&c.Properties)
	if _, ok := c.Properties[p.Name]; ok {
		return fmt.Errorf("class %s already has property %s", c.Name, p.Name)
	}
	c.Properties[p.Name] = p
	return nil
}

func (c *Class) replaceProperty(p *Property) {
	initMap(&c.Properties)
	c.Properties[p.Name] = p
}

func (c *Class) addMethod(m *Function) error {
	initMap(&c.Methods)
	if _, ok := c.Methods[m.Name]; ok {
		return fmt.Errorf("class %s already has method %s", c.Name, m.Name)
	}
	c.Methods[m.Name] = m
	return nil
}

func (c *Class) replaceMethod(m *Function) {
	initMap(&c.Methods)
	c.Methods[m.Name] = m
}

func (t *Trait) addProperty(m *Property) error {
	initMap(&t.Properties)
	if _, ok := t.Properties[m.Name]; ok {
		return fmt.Errorf("trait %s already has %s", t.Name, m.Name)
	}
	t.Properties[m.Name] = m
	return nil
}

func (t *Trait) addMethod(m *Function) error {
	initMap(&t.Methods)
	if _, ok := t.Methods[m.Name]; ok {
		return fmt.Errorf("class %s already has method %s", t.Name, m.Name)
	}
	t.Methods[m.Name] = m
	return nil
}

func initMap[M map[K]V, K comparable, V any](m *M) {
	if *m != nil {
		return
	}
	*m = make(map[K]V)
}
