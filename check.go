package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
)

// TODO: Fix this.
var fileBeingChecked = "<line>"

func check(x any) {
	dump := json.NewEncoder(os.Stdout)
	_ = dump
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *File:
		fileBeingChecked = x.Path
		check(x.Scope)
	case *scope:
		for _, stmt := range x.Stmts {
			check(stmt)
		}
	case *Debug:
		report := func(format string, args ...any) {
			fmt.Printf("%s:%d:%d: %s (DEBUG)\n",
				fileBeingChecked, x.Pos.Line, x.Pos.Column,
				fmt.Sprintf(format, args...),
			)
		}

		class := varScope[x.Var]
		if class != "" {
			report("%v is of type: %v", x.Var, class)
		} else {
			report("unknown var: %v", x.Var)
		}
	case *Stmt:
		for _, n := range x.Nodes {
			check(n)
		}
	case *AssignExpr:
		findVarType(x)
		check(x.Left)
		check(x.Right)
	case *MemberAccess:
		// dump.Encode(x)
		checkMemberAccess(x)
	case *VarExpr:
		// dump.Encode(x)
	}
}

var varScope = make(map[string]string)

func findVarType(a *AssignExpr) {
	v, ok := a.Left.(*VarExpr)
	if !ok {
		return
	}

	class := "<unknown-val>"
	switch val := a.Right.(type) {
	case *VarExpr:
		class = cmp.Or(varScope[val.Name], class)
	case *MemberAccess:
		// TODO: Don't check twice.
		class = checkMemberAccess(val)
	}
	varScope[v.Name] = class
}

func checkMemberAccess(a *MemberAccess) string {
	var x string
	switch r := a.Rcvr.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", r))
	case *VarExpr:
		if r.Name == "$this" {
			x = lastClass
		} else {
			x = cmp.Or(varScope[r.Name], "<unknown-type-of-"+r.Name+">")
		}
	case *MemberAccess:
		x = checkMemberAccess(r)
	}

	if x == "stdClass" {
		// All member access allowed.
		return x
	}

	report := func(format string, args ...any) {
		fmt.Printf("%s:%d:%d: %s\n",
			fileBeingChecked, a.Pos.Line, a.Pos.Column,
			fmt.Sprintf(format, args...),
		)
	}

	c, ok := world[x]
	if !ok {
		report("class `%v` not found", x)
		return "<unknown-class>"
	}
	m, ok := c.Members[a.Name]
	for !ok && c.Extends != "" {
		p := c.Extends
		c, ok = world[p]
		if !ok {
			report("parent `%v` not found; searching for %v", p, a.Name)
			return "<unknown-parent>"
		}
		m, ok = c.Members[a.Name]
	}
	if !ok {
		report("class member `%v::%v` does not exist", c.Name, a.Name)
		return "<unknown-member>"
	}
	return m.Class
}
