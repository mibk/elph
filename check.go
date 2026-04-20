package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"mibk.dev/elph/resolved"
	"mibk.dev/phpfmt/token"
)

func Check(file *File, a *Arbiter, warnOut io.Writer) bool {
	l := checker{
		stdout:           os.Stdout,
		stderr:           warnOut,
		arbiter:          a,
		scope:            make(map[string]resolved.Type),
		fileBeingChecked: file.Path,
		reported:         make(map[string]bool),
		ignoreLines:      file.IgnoreLines,
	}
	l.seedSuperglobals()
	for _, use := range file.UnusedUse {
		l.reportf(use.Pos, "unused use statement: %s", use.Alias)
	}
	l.check(file.Block)
	return l.hadError
}

type checker struct {
	stdout  io.Writer
	stderr  io.Writer
	arbiter *Arbiter

	scope map[string]resolved.Type

	fileBeingChecked string
	reported         map[string]bool
	ignoreLines      map[int]string

	thisClass *Class

	hadError bool
}

var ignoreTagPatterns = map[string]string{
	"property.notFound": "class property ",
	"method.notFound":   "class method ",
}

func (l *checker) reportf(pos token.Pos, format string, args ...any) {
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
	if !l.arbiter.errorMatched(msg, detail) {
		fmt.Fprintln(l.stdout, msg)
		l.hadError = true
	}
}

func (l *checker) seedSuperglobals() {
	for _, name := range phpSuperglobals {
		l.scope[name] = resolved.Array
	}
}

func (l *checker) check(x any) {
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *Class:
		backup := l.thisClass
		l.thisClass = x
		l.checkMembers("class", x.Name, &x.memberSet)
		l.thisClass = backup
		if x.Body != nil {
			l.checkClassBody(x, x.Body)
		}
	case *Trait:
		l.checkMembers("trait", x.Name, &x.memberSet)
		if x.Body != nil {
			// Trait bodies run with $this typed as stdClass — lenient,
			// since we don't know the using class yet.
			l.checkClassBody(&Class{Name: resolved.StdClass.Name}, x.Body)
		}
	case *Block:
		for _, p := range x.Params {
			l.scope[p.Name] = p.Type
		}
		for _, stmt := range x.Stmts {
			l.check(stmt)
		}
	case *ListAssign:
		typ := l.resolveExprType(x.Right)
		elem := resolved.Type(resolved.Mixed)
		if el, ok := resolved.ArrayElem(typ); ok {
			elem = el
		}
		for _, name := range x.Vars {
			if name != "" {
				l.scope[name] = elem
			}
		}
	case *Foreach:
		typ := l.resolveExprType(x.X)
		v := x.Value
		if elem, ok := resolved.ArrayElem(typ); ok {
			l.scope[v.Name] = elem
			if x.Key != nil {
				l.scope[x.Key.Name] = resolved.NewUnion(resolved.Int, resolved.String)
			}
		} else {
			l.scope[v.Name] = v.Type // fallback to "mixed"
			if x.Key != nil {
				l.scope[x.Key.Name] = resolved.Mixed
			}
		}
	case *Param:
		l.scope[x.Name] = x.Type
		l.checkType(x.Pos, x.Type, "class")
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
	case *TypeExpr:
	case *AssertExpr:
		l.scope[x.Var] = x.Type
	case *FuncCall:
		l.check(x.Args)
		l.applyByRefEffects(x)
	case *UnsetExpr:
		for _, v := range x.Vars {
			delete(l.scope, v)
		}
	case *NarrowBlock:
		backup := l.scope[x.Var]
		l.scope[x.Var] = x.Type
		l.check(x.Block)
		if x.Block.EarlyExit {
			if backup != nil {
				l.scope[x.Var] = resolved.SubtractType(backup, x.Type)
			}
		} else {
			l.scope[x.Var] = backup
		}
	}
}

// checkClassBody walks body with a fresh scope rooted in thisClass,
// seeding $this and the superglobals. Used for class/trait/interface/enum
// bodies; thisClass may be a synthetic placeholder for traits.
func (l *checker) checkClassBody(thisClass *Class, body *Block) {
	backupClass, backupScope := l.thisClass, l.scope
	l.thisClass = thisClass
	l.scope = make(map[string]resolved.Type)
	l.scope["$this"] = resolved.TypeFromName(thisClass.Name)
	l.seedSuperglobals()
	defer func() {
		l.thisClass = backupClass
		l.scope = backupScope
	}()
	for _, p := range body.Params {
		l.scope[p.Name] = p.Type
	}
	for _, stmt := range body.Stmts {
		l.check(stmt)
	}
}

func (l *checker) checkMembers(kind, name string, s *memberSet) {
	for _, d := range s.Duplicates {
		l.reportf(d.Pos, "%s %s already has %s %s", kind, name, d.Kind, d.Name)
	}
	for _, p := range s.Properties {
		if p.fromTrait {
			continue
		}
		if !l.knownType(p.Type) {
			l.reportf(p.Pos, "property %s has non-existing type %s", p.Name, p.Type)
			p.Type = resolved.Mixed // Do not report the error again.
		}
		if p.DefaultValue != nil {
			l.check(p.DefaultValue)
		}
	}
	for _, p := range s.Constants {
		if p.fromTrait {
			continue
		}
		if !l.knownType(p.Type) {
			l.reportf(p.Pos, "constant %s has non-existing type %s", p.Name, p.Type)
			p.Type = resolved.Mixed // Do not report the error again.
		}
		if p.DefaultValue != nil {
			l.check(p.DefaultValue)
		}
	}
	for _, m := range s.Methods {
		if m.fromTrait {
			continue
		}
		if !l.knownType(m.Returns) {
			l.reportf(m.Pos, "method %s returns non-existing type %s", m.Name, m.Returns)
			m.Returns = resolved.Mixed // Do not report the error again.
		}
	}
}

func (l *checker) knownType(typ resolved.Type) bool {
	switch t := typ.(type) {
	case *resolved.Builtin:
		return true
	case *resolved.Named:
		if t == resolved.StdClass || strings.Contains(t.Name, "-") {
			return true
		}
		_, ok := universe[t.Name]
		return ok
	case *resolved.Union:
		for _, m := range t.Types {
			if !l.knownType(m) {
				return false
			}
		}
		return true
	case *resolved.ArrayOf:
		return l.knownType(t.Elem)
	case *resolved.Generic:
		return l.knownType(t.Base) && l.knownType(t.Param)
	case *resolved.TypeVar:
		return true
	}
	return false
}

func (l *checker) checkType(pos token.Pos, typ resolved.Type, kind string) {
	if !l.knownType(typ) {
		l.reportf(pos, "%s %v not found", kind, typ)
	}
}

func (l *checker) findVarType(a *AssignExpr) (typ resolved.Type, checked bool) {
	switch val := a.Right.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", val))
	case *NewInstance:
		typ = l.findNewInstanceType(val.Class)
		checked = true
	case *TypeExpr:
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
			typ = resolved.Mixed
		}
	case *MemberAccess:
		typ = l.checkMemberAccess(val)
		checked = true
	case *AssignExpr:
		typ, checked = l.findVarType(val)
	case *FuncCall:
		l.check(val.Args)
		l.applyByRefEffects(val)
		typ = phpBuiltinFuncs[val.Name]
		if typ == nil {
			typ = resolved.Mixed
		}
		checked = true
	case *IndexExpr:
		if elem, ok := resolved.ArrayElem(l.resolveExprType(val.X)); ok {
			typ = elem
		} else {
			typ = resolved.Mixed
		}
	}

	if typ == resolved.Void {
		l.reportf(a.Right.Pos(), "cannot assign '%s'", typ)
		typ = resolved.Mixed
	}

	if v, ok := a.Left.(*VarExpr); ok {
		l.scope[v.Name] = typ
	}

	return typ, checked
}

func (l *checker) findNewInstanceType(x any) resolved.Type {
	mixed := resolved.Mixed
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported expr type: %T", x))
	case *TypeExpr:
		switch x.Type {
		case resolved.Self, resolved.Static:
			if l.thisClass == nil {
				l.reportf(x.ValuePos, "not in class context")
				return mixed
			}
			return resolved.TypeFromName(l.thisClass.Name)
		case resolved.Mixed:
			return x.Type
		default:
			if x.Type == resolved.StdClass {
				return x.Type
			}
			s := x.Type.String()
			if _, ok := universe[s].(*Class); !ok {
				l.reportf(x.ValuePos, "class %v not found", x.Type)
				return mixed
			}
			return x.Type
		}
	case *Class:
		return resolved.TypeFromName(x.Name)
	}
}

func (l *checker) resolveExprType(x any) resolved.Type {
	mixed := resolved.Mixed
	switch x := x.(type) {
	case *VarExpr:
		if t := l.scope[x.Name]; t != nil {
			return t
		}
		msg := fmt.Sprintf("unknown value of %s", x.Name)
		fmt.Fprintf(l.stderr, "%s:%s: [WARN] %v\n", l.fileBeingChecked, x.Pos(), msg)
		return mixed
	case *MemberAccess:
		return l.checkMemberAccess(x)
	case *FuncCall:
		l.check(x.Args)
		l.applyByRefEffects(x)
		if t, ok := phpBuiltinFuncs[x.Name]; ok {
			return t
		}
		return mixed
	}
	l.check(x)
	return mixed
}

func (l *checker) checkMemberAccess(a *MemberAccess) resolved.Type {
	mixed := resolved.Mixed
	var x resolved.Type
	switch r := a.Rcvr.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", r))
	case *TypeExpr:
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
		if r.Args != nil {
			l.check(r.Args)
		}
	case *FuncCall:
		x = phpBuiltinFuncs[r.Name]
		if x == nil {
			x = mixed
		}
	case *IndexExpr:
		t := l.resolveExprType(r.X)
		if elem, ok := resolved.ArrayElem(t); ok {
			x = elem
		} else {
			x = mixed
		}
	}

	if u, ok := x.(*resolved.Union); ok {
		kind := memberKind(a)
		var failedDisplays []string
		anyHandled := false
		for _, m := range u.Types {
			if resolved.IsBuiltin(m) {
				continue
			}
			if _, ok := m.(*resolved.ArrayOf); ok {
				continue
			}
			class, template := identFromType(m)
			result, found := l.checkClassMember(a.NamePos, class, class, a.Name, a.MethodCall, a.Static, template)
			if found {
				if result != resolved.Mixed {
					return result
				}
				anyHandled = true
				continue
			}
			displayClass := class
			if template != nil && template != resolved.Mixed {
				displayClass += "<" + template.String() + ">"
			}
			// Probe whether the user has already marked this class as
			// having magic accessors via an Elphfile Ignore pattern. If so,
			// treat this union branch as valid.
			detail := fmt.Sprintf("class %s %s::%v does not exist", kind, displayClass, a.Name)
			full := fmt.Sprintf("%s:%d:%d: %s", l.fileBeingChecked, a.NamePos.Line, a.NamePos.Column, detail)
			if l.arbiter.errorMatched(full, detail) {
				anyHandled = true
				continue
			}
			failedDisplays = append(failedDisplays, displayClass)
		}
		if !anyHandled && len(failedDisplays) > 0 {
			l.reportf(a.NamePos, "class %s %s::%v does not exist", kind, strings.Join(failedDisplays, "|"), a.Name)
		}
		return mixed
	}

	if resolved.IsTypeVar(x) {
		if l.thisClass.TemplateBound != nil {
			x = l.thisClass.TemplateBound
		} else {
			return mixed
		}
	}
	if x == resolved.Self || x == resolved.Parent {
		if l.thisClass == nil {
			l.reportf(a.NamePos, "not in class context")
			return mixed
		}
		x = resolved.TypeFromName(l.thisClass.Name)
	} else if resolved.IsBuiltin(x) {
		if x == resolved.Mixed || x == resolved.Object {
			// All member access allowed on mixed.
			return x
		}
		l.reportf(a.NamePos, "cannot call method on '%s'", x)
		return resolved.TypeFromName("<not-a-class>")
	}

	if x == resolved.StdClass {
		// All member access allowed.
		return x
	}
	if _, ok := x.(*resolved.ArrayOf); ok {
		// Member access on an array is allowed (e.g., $arr->count()).
		return mixed
	}
	class, template := identFromType(x)
	result, found := l.checkClassMember(a.NamePos, class, class, a.Name, a.MethodCall, a.Static, template)
	if !found {
		displayClass := class
		if template != nil && template != resolved.Mixed {
			displayClass += "<" + template.String() + ">"
		}
		l.reportf(a.NamePos, "class %s %s::%v does not exist", memberKind(a), displayClass, a.Name)
	}
	return result
}

// memberKind returns the kind label used in "does not exist" errors for a
// member access: "method", "const", or "property".
func memberKind(a *MemberAccess) string {
	if a.MethodCall {
		return "method"
	}
	if a.Static && !strings.HasPrefix(a.Name, "$") {
		return "const"
	}
	return "property"
}

// checkClassMember resolves a member access on a named class/trait. The
// returned bool is true when the member was handled (found directly,
// resolved through a parent, covered by DynamicProps, or the class was
// unknown — in which case an unrelated error has already been reported).
// It is false when the member is truly missing, so the caller decides how
// to report it (single-class access reports immediately; union access
// collects and emits a combined error).
func (l *checker) checkClassMember(pos token.Pos, originalClass, class string, member string, methodCall, static bool, template resolved.Type) (resolved.Type, bool) {
	mixed := resolved.Mixed
	c, ok := universe[class].(*Class)
	if !ok {
		t, ok := universe[class].(*Trait)
		if !ok {
			if class == "stdClass" {
				return resolved.StdClass, true
			}
			if key := class + "·" + l.fileBeingChecked; !l.reported[key] {
				l.reportf(pos, "class %v not found", class)
				if !static {
					l.reported[key] = true
				}
			}
			return mixed, true
		}
		// Let's check the trait as if it were a class.
		c = &Class{
			Name:      t.Name,
			memberSet: t.memberSet,
		}
	}

	for _, name := range c.Traits {
		t, ok := universe[name].(*Trait)
		if !ok {
			l.reportf(pos, "trait %v not found", name)
			continue
		}
		initMap(&c.Properties)
		for k, m := range t.Properties {
			if c.Properties[k] == nil {
				m := *m
				m.fromTrait = true
				c.Properties[k] = &m
			}
		}
		initMap(&c.Constants)
		for k, m := range t.Constants {
			if c.Constants[k] == nil {
				m := *m
				m.fromTrait = true
				c.Constants[k] = &m
			}
		}
		initMap(&c.Methods)
		for k, m := range t.Methods {
			if c.Methods[k] != nil {
				continue
			}
			m := *m
			m.fromTrait = true
			if m.Returns.String() == t.Name {
				// TODO: This is hacky, and ugly.
				m.Returns = resolved.TypeFromName(c.Name)
			}
			c.Methods[k] = &m
		}
	}
	c.Traits = nil // Mark as processed.

	var memberTyp resolved.Type

	if methodCall {
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
		return mixed, true
	} else if !isVar && static {
		if member == "class" {
			// PHP magic constant.
			return resolved.String, true
		}
		if c := c.Constants[member]; c != nil {
			memberTyp = c.Type
		}
	} else {
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
	if c.TemplateParam != nil && memberTyp != nil {
		if g, ok := memberTyp.(*resolved.Generic); ok {
			if resolved.IsTypeVar(g.Param) {
				if template != nil {
					memberTyp = &resolved.Generic{Base: g.Base, Param: template}
				} else {
					memberTyp = g.Base
				}
			}
		}
	}

	if resolved.IsTypeVar(memberTyp) && template != nil {
		memberTyp = template
	}

	if member, isVar := strings.CutPrefix(member, "$"); memberTyp == nil && static && !isVar {
		// Interfaces can define constants.
		memberTyp = findInterfaceConst(c, member)
	}

	if memberTyp == nil && c.Extends != "" {
		parent := c.Extends
		if parent == "stdClass" {
			return resolved.StdClass, true
		}
		if template == nil {
			template = c.Template
		}
		return l.checkClassMember(pos, originalClass, parent, member, methodCall, static, template)
	}
	if memberTyp == nil && c.DynamicProps {
		return mixed, true
	}
	if memberTyp == nil {
		return mixed, false
	}
	if memberTyp == resolved.Static {
		// TODO: Doesn't feel like the right place for this.
		typ := resolved.TypeFromName(originalClass)
		if template != nil {
			return &resolved.Generic{Base: typ, Param: template}, true
		}
		return typ, true
	}
	return memberTyp, true
}

func findInterfaceConst(c *Class, member string) resolved.Type {
	for _, iface := range c.Implements {
		i, ok := universe[iface].(*Class)
		if !ok {
			// Can this happen?
			continue
		}
		if c := i.Constants[member]; c != nil {
			return c.Type
		}
		if typ := findInterfaceConst(i, member); typ != nil {
			return typ
		}
	}
	return nil
}

func (l *checker) checkStaticAccess(pos token.Pos, memberName string, isStatic, accessStatic, methodCall bool) {
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

