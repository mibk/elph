package main

import (
	"fmt"

	"mibk.dev/elph/resolved"
	"mibk.dev/phpfmt/token"
)

type File struct {
	Path        string
	Block       *Block
	IgnoreLines map[int]string // line number → @phpstan-ignore tag
}

type UseStmt struct {
	Namespace string
	Alias     string
}

type Block struct {
	Params []*Param
	Stmts  []*Stmt
}

type Param struct {
	Pos  token.Pos
	Name string
	Type resolved.Type

	DefaultValue *Stmt // or nil
}

type Debug struct {
	Var string
	Pos token.Pos
}

type Stmt struct {
	Nodes []any
}

type Class struct {
	Name          string
	TemplateParam string // declared via @template (e.g. "T", "TItem")
	TemplateBound resolved.Type // bound from @template T of X, or nil
	Template      resolved.Type // concrete type from @extends, or nil
	Extends       string // or empty
	Implements    []string
	Traits        []string
	Properties    map[string]*Property
	Constants     map[string]*Property
	Methods       map[string]*Function

	SourceFile string
}

type Trait struct {
	Name       string
	Properties map[string]*Property
	Constants  map[string]*Property
	Methods    map[string]*Function

	SourceFile string
}

type Property struct {
	Pos    token.Pos
	Name   string
	Type   resolved.Type
	Static bool

	DefaultValue *Stmt // or nil
}

type Function struct {
	Pos     token.Pos
	Name    string
	Returns resolved.Type
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

type ValueExpr struct {
	V    token.Pos
	Type resolved.Type
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
	Args       *Block // method call arguments, or nil
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
	Type resolved.Type
}

func (e *AssertExpr) Pos() token.Pos { return e.Fn }

type UnsetExpr struct {
	Fn   token.Pos
	Vars []string
}

func (e *UnsetExpr) Pos() token.Pos { return e.Fn }

// NarrowBlock is a block where a variable is temporarily narrowed
// to a more specific type. The checker restores the original type
// after the block.
type NarrowBlock struct {
	Var       string
	Type      resolved.Type
	Block     *Block
	EarlyExit bool
}

////////////

type typeDecl interface {
	sourceFile() string
	addProperty(m *Property) error
	addConstant(m *Property) error
	addMethod(m *Function) error
}

func (c *Class) sourceFile() string { return c.SourceFile }

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

func (c *Class) addConstant(p *Property) error {
	initMap(&c.Constants)
	if _, ok := c.Constants[p.Name]; ok {
		return fmt.Errorf("class %s already has constant %s", c.Name, p.Name)
	}
	c.Constants[p.Name] = p
	return nil
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

func (t *Trait) sourceFile() string { return t.SourceFile }

func (t *Trait) addProperty(m *Property) error {
	initMap(&t.Properties)
	if _, ok := t.Properties[m.Name]; ok {
		return fmt.Errorf("trait %s already has property %s", t.Name, m.Name)
	}
	t.Properties[m.Name] = m
	return nil
}

func (t *Trait) addConstant(p *Property) error {
	initMap(&t.Constants)
	if _, ok := t.Constants[p.Name]; ok {
		return fmt.Errorf("trait %s already has constant %s", t.Name, p.Name)
	}
	t.Constants[p.Name] = p
	return nil
}

func (t *Trait) addMethod(m *Function) error {
	initMap(&t.Methods)
	if _, ok := t.Methods[m.Name]; ok {
		return fmt.Errorf("trait %s already has method %s", t.Name, m.Name)
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
