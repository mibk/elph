package main

import (
	"mibk.dev/elph/resolved"
	"mibk.dev/phpfmt/token"
)

type File struct {
	Path        string
	Block       *Block
	IgnoreLines map[int]string // line number → @phpstan-ignore tag
	UnusedUse   []UseStmt
}

type UseStmt struct {
	Pos       token.Pos
	Namespace string
	Alias     string
}

type Block struct {
	Params    []*Param
	Stmts     []*Stmt
	EarlyExit bool // last stmt is return/throw/break/continue
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
	Nodes     []any
	EarlyExit bool // contains return/throw/break/continue
}

type Class struct {
	Name          string
	TemplateParam *resolved.TypeVar // declared via @template, or nil
	TemplateBound resolved.Type     // bound from @template T of X, or nil
	Template      resolved.Type     // concrete type from @extends, or nil
	Extends       string            // or empty
	Implements    []string
	DynamicProps  bool
	IsEnum        bool
	BackedType    resolved.Type // nil for pure enums, resolved.Int or resolved.String for backed
	Traits        []string
	Body          *Block // method bodies; nil for interfaces with no bodies
	memberSet

	SourceFile string
}

type Trait struct {
	Name string
	Body *Block
	memberSet

	SourceFile string
}

type memberSet struct {
	Properties map[string]*Property
	Constants  map[string]*Property
	Methods    map[string]*Method
	Duplicates []Duplicate
}

type Duplicate struct {
	Pos  token.Pos
	Kind string // "method", "property", or "constant"
	Name string
}

type Property struct {
	Pos    token.Pos
	Name   string
	Type   resolved.Type
	Static bool

	DefaultValue *Stmt // or nil

	fromTrait bool
}

type Method struct {
	Pos     token.Pos
	Name    string
	Returns resolved.Type
	Static  bool

	fromTrait bool
}

type Foreach struct {
	X     Expr
	Key   *Param // or nil if no key
	Value Param
}

type ListAssign struct {
	Vars  []string // variable names (may include "" for skipped positions)
	Right Expr
}

type Expr interface {
	Pos() token.Pos
}

type NewInstance struct {
	New   token.Pos
	Class any
}

func (e *NewInstance) Pos() token.Pos { return e.New }

type TypeExpr struct {
	ValuePos token.Pos
	Type     resolved.Type
}

func (e *TypeExpr) Pos() token.Pos { return e.ValuePos }

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

type FuncCall struct {
	NamePos token.Pos
	Name    string
	Args    *Block
}

func (e *FuncCall) Pos() token.Pos { return e.NamePos }

// NarrowBlock is a block where a variable is temporarily narrowed
// to a more specific type. The checker restores the original type
// after the block.
type NarrowBlock struct {
	Var   string
	Type  resolved.Type
	Block *Block
}

////////////

type typeDecl interface {
	sourceFile() string
	addProperty(m *Property)
	addConstant(m *Property)
	addMethod(m *Method)
}

func (c *Class) sourceFile() string { return c.SourceFile }
func (t *Trait) sourceFile() string { return t.SourceFile }

func (s *memberSet) addProperty(p *Property) {
	initMap(&s.Properties)
	if _, ok := s.Properties[p.Name]; ok {
		s.Duplicates = append(s.Duplicates, Duplicate{Pos: p.Pos, Kind: "property", Name: p.Name})
		return
	}
	s.Properties[p.Name] = p
}

func (s *memberSet) replaceProperty(p *Property) {
	initMap(&s.Properties)
	s.Properties[p.Name] = p
}

func (s *memberSet) addConstant(p *Property) {
	initMap(&s.Constants)
	if _, ok := s.Constants[p.Name]; ok {
		s.Duplicates = append(s.Duplicates, Duplicate{Pos: p.Pos, Kind: "constant", Name: p.Name})
		return
	}
	s.Constants[p.Name] = p
}

func (s *memberSet) addMethod(m *Method) {
	initMap(&s.Methods)
	if _, ok := s.Methods[m.Name]; ok {
		s.Duplicates = append(s.Duplicates, Duplicate{Pos: m.Pos, Kind: "method", Name: m.Name})
		return
	}
	s.Methods[m.Name] = m
}

func (s *memberSet) replaceMethod(m *Method) {
	initMap(&s.Methods)
	s.Methods[m.Name] = m
}

func initMap[M map[K]V, K comparable, V any](m *M) {
	if *m != nil {
		return
	}
	*m = make(map[K]V)
}
