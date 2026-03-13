// Package resolved defines resolved types used by the checker.
package resolved

import "strings"

// Sentinel values for commonly used built-in types.
var (
	// Special types.
	Mixed = &Builtin{Name: "mixed"}
	Void  = &Builtin{Name: "void"}
	Never = &Builtin{Name: "never"}
	Null  = &Builtin{Name: "null"}

	// Self-referential types.
	Self   = &Builtin{Name: "self"}
	Static = &Builtin{Name: "static"}
	Parent = &Builtin{Name: "parent"}

	// Scalar types.
	String = &Builtin{Name: "string"}
	Int    = &Builtin{Name: "int"}
	Float  = &Builtin{Name: "float"}
	Bool   = &Builtin{Name: "bool"}
	True   = &Builtin{Name: "true"}
	False  = &Builtin{Name: "false"}

	// Compound types.
	Object   = &Builtin{Name: "object"}
	Array    = &Builtin{Name: "array"}
	Iterable = &Builtin{Name: "iterable"}
	Callable = &Builtin{Name: "callable"}
	Resource = &Builtin{Name: "resource"}
)

// builtinTypes maps built-in PHP type names to their sentinel values.
var builtinTypes = map[string]*Builtin{
	// Special types.
	"mixed": Mixed,
	"void":  Void,
	"never": Never,
	"null":  Null,

	// Self-referential types.
	"self":   Self,
	"static": Static,
	"parent": Parent,

	// Scalar types.
	"string": String,
	"int":    Int,
	"float":  Float,
	"bool":   Bool,
	"true":   True,
	"false":  False,

	// Compound types.
	"object":   Object,
	"array":    Array,
	"iterable": Iterable,
	"callable": Callable,
	"resource": Resource,

	// Aliases.
	"double":  Float,
	"integer": Int,
	"boolean": Bool,
}

// Type represents a resolved PHP type.
type Type interface {
	typ()
	String() string
}

// Named is a fully-qualified class/interface/trait/enum name.
type Named struct {
	Name string // e.g. "App\Models\User"
}

func (*Named) typ()             {}
func (n *Named) String() string { return n.Name }

// Builtin is a built-in PHP type like "mixed", "string", "int", etc.
type Builtin struct {
	Name string
}

func (*Builtin) typ()             {}
func (b *Builtin) String() string { return b.Name }

// Union is a union of two or more types.
type Union struct {
	Types []Type
}

func (*Union) typ() {}
func (u *Union) String() string {
	parts := make([]string, len(u.Types))
	for i, t := range u.Types {
		parts[i] = t.String()
	}
	return strings.Join(parts, "|")
}

// ArrayOf represents an array type with a known element type.
type ArrayOf struct {
	Elem Type
}

func (*ArrayOf) typ()             {}
func (a *ArrayOf) String() string { return a.Elem.String() + "[]" }

// Generic represents a generic type like Collection<User>.
type Generic struct {
	Base  Type
	Param Type
}

func (*Generic) typ() {}
func (g *Generic) String() string {
	return g.Base.String() + "<" + g.Param.String() + ">"
}

// TypeVar represents a template parameter like "T".
type TypeVar struct {
	Name string
}

func (*TypeVar) typ()             {}
func (v *TypeVar) String() string { return v.Name }

// IsTypeVar reports whether typ is a TypeVar (template parameter).
func IsTypeVar(typ Type) bool {
	_, ok := typ.(*TypeVar)
	return ok
}

// IsBuiltin reports whether typ is a Builtin type.
func IsBuiltin(typ Type) bool {
	_, ok := typ.(*Builtin)
	return ok
}

// IsBuiltinName reports whether name is a built-in PHP type name.
func IsBuiltinName(name string) bool {
	_, ok := builtinTypes[name]
	return ok
}

// TypeFromName returns a Builtin for built-in PHP type names,
// or a Named for everything else.
func TypeFromName(name string) Type {
	if name == "" {
		return Mixed
	}
	if b, ok := builtinTypes[name]; ok {
		return b
	}
	if n, ok := namedCache[name]; ok {
		return n
	}
	n := &Named{Name: name}
	namedCache[name] = n
	return n
}

// StdClass is the sentinel for PHP's stdClass. Member access
// on stdClass is always allowed (dynamic properties).
var StdClass = &Named{Name: "stdClass"}

var namedCache = map[string]*Named{
	"stdClass": StdClass,
}

// ArrayElem returns the element type if typ is an array.
// For unions, it collects element types from all array members.
func ArrayElem(typ Type) (Type, bool) {
	if a, ok := typ.(*ArrayOf); ok {
		return a.Elem, true
	}
	if u, ok := typ.(*Union); ok {
		var elems []Type
		for _, m := range u.Types {
			if a, ok := m.(*ArrayOf); ok {
				elems = append(elems, a.Elem)
			}
		}
		if len(elems) == 0 {
			return nil, false
		}
		return NewUnion(elems...), true
	}
	return nil, false
}

// SubtractType removes excluded from typ (which may be a union).
// Returns Mixed if nothing remains.
func SubtractType(typ Type, excluded Type) Type {
	u, ok := typ.(*Union)
	if !ok {
		if typ == excluded || typ.String() == excluded.String() {
			return Mixed
		}
		return typ
	}
	var remaining []Type
	ex := excluded.String()
	for _, t := range u.Types {
		if t != excluded && t.String() != ex {
			remaining = append(remaining, t)
		}
	}
	switch len(remaining) {
	case 0:
		return Mixed
	case 1:
		return remaining[0]
	default:
		return &Union{Types: remaining}
	}
}

// NewUnion creates a union, flattening nested unions and collapsing
// single-element results.
func NewUnion(types ...Type) Type {
	var flat []Type
	for _, t := range types {
		if u, ok := t.(*Union); ok {
			flat = append(flat, u.Types...)
		} else {
			flat = append(flat, t)
		}
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return &Union{Types: flat}
}
