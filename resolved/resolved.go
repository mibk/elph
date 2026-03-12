// Package resolved defines resolved types used by the checker.
package resolved

import "strings"

// Sentinel values for commonly used basic types.
var (
	Mixed      = &Basic{Name: "mixed"}
	Null       = &Basic{Name: "null"}
	Self       = &Basic{Name: "self"}
	Static     = &Basic{Name: "static"}
	String     = &Basic{Name: "string"}
	Void       = &Basic{Name: "void"}
	Int        = &Basic{Name: "int"}
	Float      = &Basic{Name: "float"}
	Bool       = &Basic{Name: "bool"}
	True       = &Basic{Name: "true"}
	False      = &Basic{Name: "false"}
	Object     = &Basic{Name: "object"}
	BasicArray = &Basic{Name: "array"}
	Never      = &Basic{Name: "never"}
	Callable   = &Basic{Name: "callable"}
	Resource   = &Basic{Name: "resource"}
	Parent     = &Basic{Name: "parent"}
)

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

// Basic is a built-in PHP type like "mixed", "string", "int", etc.
type Basic struct {
	Name string
}

func (*Basic) typ()             {}
func (b *Basic) String() string { return b.Name }

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

// Array represents an array type with a known element type.
type Array struct {
	Elem Type
}

func (*Array) typ()             {}
func (a *Array) String() string { return a.Elem.String() + "[]" }

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

// IsBasic reports whether typ is a Basic type.
func IsBasic(typ Type) bool {
	_, ok := typ.(*Basic)
	return ok
}

// IsBasicName reports whether name is a built-in PHP type name.
func IsBasicName(name string) bool {
	switch name {
	case "void", "never", "self", "static", "parent",
		"mixed", "string", "int", "float", "bool", "true", "false",
		"object", "array", "callable", "resource":
		return true
	}
	return false
}

// TypeFromName returns a Basic for built-in PHP type names,
// or a Named for everything else.
func TypeFromName(name string) Type {
	switch name {
	case "mixed":
		return Mixed
	case "null":
		return Null
	case "self":
		return Self
	case "static":
		return Static
	case "string":
		return String
	case "void":
		return Void
	case "int":
		return Int
	case "float":
		return Float
	case "bool":
		return Bool
	case "true":
		return True
	case "false":
		return False
	case "object":
		return Object
	case "array":
		return BasicArray
	case "never":
		return Never
	case "callable":
		return Callable
	case "resource":
		return Resource
	case "parent":
		return Parent
	}
	if n, ok := namedCache[name]; ok {
		return n
	}
	n := &Named{Name: name}
	namedCache[name] = n
	return n
}

var namedCache = make(map[string]*Named)

// ArrayElem returns the element type if typ is an array.
// For unions, it collects element types from all array members.
func ArrayElem(typ Type) (Type, bool) {
	if a, ok := typ.(*Array); ok {
		return a.Elem, true
	}
	if u, ok := typ.(*Union); ok {
		var elems []Type
		for _, m := range u.Types {
			if a, ok := m.(*Array); ok {
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
