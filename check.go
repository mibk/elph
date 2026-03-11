package main

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"mibk.dev/elph/resolved"
	"mibk.dev/phpfmt/token"
)

// hasErrors is set to true when any error is reported.
var hasErrors = false

func Check(file *File, a *Arbiter, warnOut io.Writer) {
	l := linter{
		stdout:           os.Stdout,
		stderr:           warnOut,
		arbiter:          a,
		scope:            make(map[string]resolved.Type),
		fileBeingChecked: file.Path,
		reported:         make(map[string]bool),
		ignoreLines:      file.IgnoreLines,
	}
	l.check(file.Block)
}

type linter struct {
	stdout  io.Writer
	stderr  io.Writer
	arbiter *Arbiter

	scope map[string]resolved.Type

	fileBeingChecked string
	reported         map[string]bool
	ignoreLines      map[int]string

	thisClass *Class
	nextClass *Class
	pushScope bool
}

var ignoreTagPatterns = map[string]string{
	"property.notFound": "class property ",
	"method.notFound":   "class method ",
}

func (l *linter) reportf(pos token.Pos, format string, args ...any) {
	detail := fmt.Sprintf(format, args...)
	if tag := l.ignoreLines[pos.Line]; tag != "" {
		if prefix, ok := ignoreTagPatterns[tag]; ok && strings.HasPrefix(detail, prefix) {
			return
		}
	}
	msg := fmt.Sprintf("%s:%d:%d: %s",
		l.fileBeingChecked, pos.Line, pos.Column,
		detail,
	)
	if !l.arbiter.errorMatched(msg) {
		fmt.Fprintln(l.stdout, msg)
		hasErrors = true
	}
}

func (l *linter) check(x any) {
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *Class:
		backup := l.thisClass
		l.thisClass = x
		for _, p := range x.Properties {
			if x.TemplateParam == "" || p.Type.String() != x.TemplateParam {
				if !l.existsType(p.Type) {
					l.reportf(p.Pos, "property %s has non-existing type %s", p.Name, p.Type)
					p.Type = &resolved.Basic{Name: "mixed"} // Do not report the error again.
				}
			}
			if p.DefaultValue != nil {
				l.check(p.DefaultValue)
			}
		}
		for _, p := range x.Constants {
			if x.TemplateParam == "" || p.Type.String() != x.TemplateParam {
				if !l.existsType(p.Type) {
					l.reportf(p.Pos, "constant %s has non-existing type %s", p.Name, p.Type)
					p.Type = &resolved.Basic{Name: "mixed"} // Do not report the error again.
				}
			}
			if p.DefaultValue != nil {
				l.check(p.DefaultValue)
			}
		}
		for _, m := range x.Methods {
			if x.TemplateParam == "" || m.Returns.String() != x.TemplateParam {
				if !l.existsType(m.Returns) {
					l.reportf(m.Pos, "method %s returns non-existing type %s", m.Name, m.Returns)
					m.Returns = &resolved.Basic{Name: "mixed"} // Do not report the error again.
				}
			}
		}

		l.thisClass = backup
		l.nextClass = x
		l.pushScope = true
	case *Trait:
		l.nextClass = &Class{Name: "stdClass"} // Ignore
		l.pushScope = true
	case *Block:
		if l.pushScope {
			backupClass := l.thisClass
			l.thisClass = l.nextClass
			backupScope := l.scope
			l.scope = make(map[string]resolved.Type)
			defer func() {
				l.thisClass = backupClass
				l.scope = backupScope
			}()
			l.scope["$this"] = toType(l.thisClass.Name)
			l.pushScope = false
		}
		for _, p := range x.Params {
			l.scope[p.Name] = p.Type
		}
		for _, stmt := range x.Stmts {
			l.check(stmt)
		}
	case *Foreach:
		typ := l.resolveExprType(x.X)
		v := x.Value
		if elem, ok := resolved.ArrayElem(typ); ok {
			l.scope[v.Name] = elem
		} else {
			l.scope[v.Name] = v.Type // fallback to "mixed"
		}
	case *Param:
		l.scope[x.Name] = x.Type
		if l.thisClass == nil || l.thisClass.TemplateParam == "" || x.Type.String() != l.thisClass.TemplateParam {
			l.checkType(x.Pos, x.Type, "class")
		}
		if x.DefaultValue != nil {
			l.check(x.DefaultValue)
		}
	case *Debug:
		typ := l.scope[x.Var]
		if typ != nil {
			l.reportf(x.Pos, "%v is of type: %v (DEBUG)", x.Var, typ)
		} else {
			l.reportf(x.Pos, "unknown var: %v (DEBUG)", x.Var)
		}
	case *NewInstance:
		l.findNewInstanceType(x.Class)
		l.check(x.Class)
	case *Stmt:
		for _, n := range x.Nodes {
			l.check(n)
		}
	case *AssignExpr:
		l.check(x.Left)
		if _, checked := l.findVarType(x); !checked {
			l.check(x.Right)
		}
	case *MemberAccess:
		l.checkMemberAccess(x)
		if x.Args != nil {
			l.check(x.Args)
		}
	case *IndexExpr:
		l.check(x.X)
	case *VarExpr:
	case *ValueExpr:
	case *AssertExpr:
		l.scope[x.Var] = x.Type
	case *UnsetExpr:
		for _, v := range x.Vars {
			delete(l.scope, v)
		}
	case *NarrowBlock:
		backup := l.scope[x.Var]
		l.scope[x.Var] = x.Type
		l.check(x.Block)
		if x.EarlyExit {
			if backup != nil {
				l.scope[x.Var] = resolved.SubtractType(backup, x.Type)
			}
		} else {
			l.scope[x.Var] = backup
		}
	}
}

func (l *linter) existsType(typ resolved.Type) bool {
	switch t := typ.(type) {
	case *resolved.Basic:
		return true
	case *resolved.Named:
		if t.Name == "stdClass" || strings.Contains(t.Name, "-") {
			return true
		}
		_, ok := universe[Ident(t.Name)]
		return ok
	case *resolved.Union:
		for _, m := range t.Types {
			if !l.existsType(m) {
				return false
			}
		}
		return true
	case *resolved.Array:
		return l.existsType(t.Elem)
	case *resolved.Generic:
		return true // TODO: Check generics too
	case *resolved.TypeVar:
		return true
	}
	return false
}

func (l *linter) checkType(pos token.Pos, typ resolved.Type, kind string) {
	if !l.existsType(typ) {
		l.reportf(pos, "%s %v not found", kind, typ)
	}
}

func (l *linter) findVarType(a *AssignExpr) (typ resolved.Type, checked bool) {
	switch val := a.Right.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", val))
	case *NewInstance:
		typ = l.findNewInstanceType(val.Class)
		checked = true
	case *ValueExpr:
		typ = val.Type
	case *VarExpr:
		if strings.HasPrefix(val.Name, "$") {
			typ = l.scope[val.Name]
			if typ == nil {
				msg := fmt.Sprintf("unknown value of %s", val.Name)
				fmt.Fprintf(l.stderr, "%s:%s: [WARN] %v\n", l.fileBeingChecked, a.Right.Pos(), msg)
			}
		}
		// If unknown, hope for the best.
		if typ == nil {
			typ = &resolved.Basic{Name: "mixed"}
		}
	case *MemberAccess:
		typ = l.checkMemberAccess(val)
		checked = true
	case *AssignExpr:
		typ, checked = l.findVarType(val)
	case *IndexExpr:
		// TODO: Fix this.
		typ = &resolved.Basic{Name: "mixed"}
	}

	if typ.String() == "void" {
		l.reportf(a.Right.Pos(), "cannot assign '%s'", typ)
		typ = &resolved.Basic{Name: "mixed"}
	}

	if v, ok := a.Left.(*VarExpr); ok {
		l.scope[v.Name] = typ
	}

	return typ, checked
}

func (l *linter) findNewInstanceType(x any) resolved.Type {
	mixed := &resolved.Basic{Name: "mixed"}
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported expr type: %T", x))
	case *ValueExpr:
		s := x.Type.String()
		switch s {
		case "self", "static":
			if l.thisClass == nil {
				l.reportf(x.V, "not in class context")
				return mixed
			}
			return toType(l.thisClass.Name)
		case "stdClass", "mixed":
			return x.Type
		}
		if _, ok := universe[Ident(s)].(*Class); !ok {
			l.reportf(x.V, "class %v not found", x.Type)
			return mixed
		}
		return x.Type
	case *Class:
		return toType(x.Name)
	}
}

func (l *linter) resolveExprType(x any) resolved.Type {
	mixed := &resolved.Basic{Name: "mixed"}
	switch x := x.(type) {
	case *VarExpr:
		if t := l.scope[x.Name]; t != nil {
			return t
		}
	case *MemberAccess:
		return l.checkMemberAccess(x)
	}
	l.check(x)
	return mixed
}

func (l *linter) checkMemberAccess(a *MemberAccess) resolved.Type {
	mixed := &resolved.Basic{Name: "mixed"}
	var x resolved.Type
	switch r := a.Rcvr.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", r))
	case *ValueExpr:
		x = r.Type
	case *VarExpr:
		if override, ok := l.scope[r.Name+"->"+a.Name]; ok {
			return override
		}
		// TODO: For now, let's default to mixed.
		x = l.scope[r.Name]
		if x == nil {
			x = mixed
		}
	case *MemberAccess:
		x = l.checkMemberAccess(r)
	case *IndexExpr:
		t := l.resolveExprType(r.X)
		if elem, ok := resolved.ArrayElem(t); ok {
			x = elem
		} else {
			x = mixed
		}
	}

	if u, ok := x.(*resolved.Union); ok {
		for _, m := range u.Types {
			if resolved.IsBasic(m) {
				continue
			}
			if _, ok := m.(*resolved.Array); ok {
				continue
			}
			name := toIdent(m)
			result := l.checkClassMember(a.NamePos, name, name, a.Name, a.MethodCall, a.Static, "")
			if result.String() != "mixed" {
				return result
			}
		}
		return mixed
	}

	s := x.String()
	if l.thisClass != nil && l.thisClass.TemplateParam != "" && s == l.thisClass.TemplateParam {
		if l.thisClass.TemplateBound != "" {
			x = toType(l.thisClass.TemplateBound)
			s = x.String()
		} else {
			return mixed
		}
	}

	if s == "self" || s == "parent" {
		// TODO: This is definitely a hack. Fix it.
		x = toType(cmp.Or(l.thisClass, l.nextClass).Name)
		s = x.String()
	} else if resolved.IsBasic(x) {
		if s == "mixed" || s == "object" {
			// All member access allowed on mixed.
			return x
		}
		l.reportf(a.NamePos, "cannot call method on '%s'", x)
		return &resolved.Named{Name: "<not-a-class>"}
	}

	if s == "stdClass" {
		// All member access allowed.
		return x
	}
	if _, ok := x.(*resolved.Array); ok {
		// Member access on an array is allowed (e.g., $arr->count()).
		return mixed
	}
	id := toIdent(x)
	return l.checkClassMember(a.NamePos, id, id, a.Name, a.MethodCall, a.Static, "")
}

func (l *linter) checkClassMember(pos token.Pos, originalClass, class Ident, member string, methodCall, static bool, template Ident) resolved.Type {
	mixed := &resolved.Basic{Name: "mixed"}
	if base, t, ok := strings.Cut(string(class), "<>"); ok {
		class, template = Ident(base), Ident(t)
	}

	// TODO: Different error if entity exists but is not a class?
	c, ok := universe[class].(*Class)
	if !ok {
		t, ok := universe[class].(*Trait)
		if !ok {
			if class == "stdClass" {
				// TODO: This hack is on too many places. Fix it.
				return toType(class)
			}
			if key := string(class) + "·" + l.fileBeingChecked; !l.reported[key] {
				l.reportf(pos, "class %v not found", class)
				if !static {
					l.reported[key] = true
				}
			}
			return mixed
		}
		// Let's check the trait as if it were a class.
		c = &Class{
			Name:       t.Name,
			Properties: t.Properties,
			Constants:  t.Constants,
			Methods:    t.Methods,
		}
	}

	for _, name := range c.Traits {
		t, ok := universe[name].(*Trait)
		if !ok {
			l.reportf(pos, "trait %v not found", name)
			continue
		}
		for _, m := range t.Properties {
			// TODO: Check whether property not already defined?
			c.addProperty(m)
		}
		for _, m := range t.Constants {
			c.addConstant(m)
		}
		for _, m := range t.Methods {
			// TODO: Check whether method not already defined?
			m := *m
			if m.Returns.String() == string(t.Name) {
				// TODO: This is hacky, and ugly.
				m.Returns = toType(c.Name)
			}
			c.addMethod(&m)
		}
	}
	c.Traits = nil // Mark as processed.

	var memberTyp resolved.Type
	var memberKind string

	if methodCall {
		memberKind = "method"
		if m := c.Methods[member]; m != nil {
			l.checkStaticAccess(pos, member, m.Static, static, true)
			memberTyp = m.Returns
		} else if p := c.Properties[member]; p != nil {
			l.checkStaticAccess(pos, member, p.Static, static, false)
			// TODO: For now, let's assume the property is a callable.
			memberTyp = mixed
		} else if strings.HasPrefix(member, "$") {
			// We cannot decide.
			memberTyp = mixed
		}
	} else if member, isVar := strings.CutPrefix(member, "$"); isVar && !static {
		// This is stupid dynamic $foo->$bar call. Ignore it.
		return mixed
	} else if !isVar && static {
		if member == "class" {
			// PHP magic constant.
			return &resolved.Basic{Name: "string"}
		}
		memberKind = "const"
		if c := c.Constants[member]; c != nil {
			memberTyp = c.Type
		}
	} else {
		memberKind = "property"
		if p := c.Properties[member]; p != nil {
			l.checkStaticAccess(pos, member, p.Static, static, false)
			memberTyp = p.Type
		} else {
			// TODO: Let's assume, for now,
			// that any property might be a get method.
			getter := []rune(member)
			getter[0] = unicode.ToUpper(getter[0])
			if m := c.Methods["get"+string(getter)]; m != nil && m.Static == static {
				memberTyp = m.Returns
			}
		}
	}

	// Hack for generics.
	if c.TemplateParam != "" && memberTyp != nil {
		if g, ok := memberTyp.(*resolved.Generic); ok {
			if g.Param.String() == c.TemplateParam {
				if template != "" {
					memberTyp = &resolved.Generic{Base: g.Base, Param: toType(template)}
				} else {
					memberTyp = g.Base
				}
			}
		}
	}

	if c.TemplateParam != "" && memberTyp != nil && memberTyp.String() == c.TemplateParam && template != "" {
		memberTyp = toType(template)
	}

	if member, isVar := strings.CutPrefix(member, "$"); memberTyp == nil && static && !isVar {
		// Interfaces can define constants.
		memberTyp = findImplementorsConstType(c, member)
	}

	if memberTyp == nil && c.Extends != "" {
		parent := c.Extends
		if parent == "stdClass" {
			// All good.
			// TODO: Really?
			return toType(parent)
		}
		template = cmp.Or(template, c.Template)
		return l.checkClassMember(pos, originalClass, parent, member, methodCall, static, template)
	}
	if memberTyp == nil {
		l.reportf(pos, "class %s %v::%v does not exist", memberKind, originalClass, member)
		return mixed
	}
	if memberTyp.String() == "static" {
		// TODO: Doesn't feel like the right place for this.
		return toType(originalClass)
	}
	return memberTyp
}

func findImplementorsConstType(c *Class, member string) resolved.Type {
	for _, iface := range c.Implements {
		i, ok := universe[iface].(*Class)
		if !ok {
			// Can this happen?
			continue
		}
		if c := i.Constants[member]; c != nil {
			return c.Type
		}
		if typ := findImplementorsConstType(i, member); typ != nil {
			return typ
		}
	}
	return nil
}

func (l *linter) checkStaticAccess(pos token.Pos, memberName string, isStatic, accessStatic, methodCall bool) {
	if isStatic == accessStatic {
		return
	}

	verb, obj := "access", "property"
	if methodCall {
		verb, obj = "call", "method"
	}
	if isStatic {
		l.reportf(pos, "cannot %s static %s '%s' via object instance", verb, obj, memberName)
	} else {
		if methodCall {
			// Prevent issues with parent::foo etc.
			return
		}
		l.reportf(pos, "cannot %s instance %s '%s' statically", verb, obj, memberName)
	}
}
