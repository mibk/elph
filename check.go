package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"mibk.dev/phpfmt/token"
)

func Check(x any) {
	l := linter{
		stdout:           os.Stdout,
		scope:            make(map[string]string),
		fileBeingChecked: "<line>",
	}
	l.check(x)
}

type linter struct {
	stdout io.Writer
	scope  map[string]string

	// TODO: Fix this.
	fileBeingChecked string

	thisClass *Class
}

func (l *linter) reportf(pos token.Pos, format string, args ...any) {
	fmt.Fprintf(l.stdout, "%s:%d:%d: %s\n",
		l.fileBeingChecked, pos.Line, pos.Column,
		fmt.Sprintf(format, args...),
	)
}

func (l *linter) check(x any) {
	dump := json.NewEncoder(os.Stdout)
	_ = dump
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *File:
		l.fileBeingChecked = x.Path
		l.check(x.Scope)
	case *Class:
		l.thisClass = x
	case *scope:
		for _, stmt := range x.Stmts {
			l.check(stmt)
		}
	case *Debug:
		class := l.scope[x.Var]
		if class != "" {
			l.reportf(x.Pos, "%v is of type: %v (DEBUG)", x.Var, class)
		} else {
			l.reportf(x.Pos, "unknown var: %v (DEBUG)", x.Var)
		}
	case *Stmt:
		for _, n := range x.Nodes {
			l.check(n)
		}
	case *AssignExpr:
		l.findVarType(x)
		l.check(x.Left)
		l.check(x.Right)
	case *MemberAccess:
		// dump.Encode(x)
		l.checkMemberAccess(x)
	case *VarExpr:
		// dump.Encode(x)
	}
}

func (l *linter) findVarType(a *AssignExpr) {
	v, ok := a.Left.(*VarExpr)
	if !ok {
		return
	}

	class := "<unknown-val>"
	switch val := a.Right.(type) {
	case *VarExpr:
		class = cmp.Or(l.scope[val.Name], class)
	case *MemberAccess:
		// TODO: Don't check twice.
		class = l.checkMemberAccess(val)
	}
	l.scope[v.Name] = class
}

func (l *linter) checkMemberAccess(a *MemberAccess) string {
	var x string
	switch r := a.Rcvr.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", r))
	case *VarExpr:
		if r.Name == "$this" && l.thisClass != nil {
			x = l.thisClass.Name
		} else {
			x = cmp.Or(l.scope[r.Name], "<unknown-type-of-"+r.Name+">")
		}
	case *MemberAccess:
		x = l.checkMemberAccess(r)
	}

	if x == "stdClass" {
		// All member access allowed.
		return x
	}

	c, ok := world[x]
	if !ok {
		l.reportf(a.Pos, "class `%v` not found", x)
		return "<unknown-class>"
	}
	m, ok := c.Members[a.Name]
	for !ok && c.Extends != "" {
		p := c.Extends
		c, ok = world[p]
		if !ok {
			l.reportf(a.Pos, "parent `%v` not found; searching for %v", p, a.Name)
			return "<unknown-parent>"
		}
		m, ok = c.Members[a.Name]
	}
	if !ok {
		l.reportf(a.Pos, "class member `%v::%v` does not exist", c.Name, a.Name)
		return "<unknown-member>"
	}
	return m.Class
}
