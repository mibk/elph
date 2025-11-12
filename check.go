package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

func check(x any) {
	dump := json.NewEncoder(os.Stdout)
	_ = dump
	switch x := x.(type) {
	default:
		panic(fmt.Sprintf("unsupported type: %T", x))
	case *File:
		check(x.Scope)
	case *scope:
		for _, stmt := range x.Nodes {
			check(stmt)
		}
	case *stmt:
		for _, n := range x.Nodes {
			check(n)
		}
	case *MemberAccess:
		// dump.Encode(x)
		checkMemberAccess(x)
	case *VarExpr:
		// dump.Encode(x)
	}
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
			// TODO: Fix this.
			x = "stdClass"
		}
	case *MemberAccess:
		x = checkMemberAccess(r)
	}

	if x == "stdClass" {
		// All member access allowed.
		return x
	}

	c, ok := world[x]
	if !ok {
		log.Printf("class `%v` not found", x)
		return "<unknown-class>"
	}
	m, ok := c.Members[a.Name]
	for !ok && c.Extends != "" {
		p := c.Extends
		c, ok = world[p]
		if !ok {
			log.Printf("parent `%v` not found; searching for %v", p, a.Name)
			return "<unknown-parent>"
		}
		m, ok = c.Members[a.Name]
	}
	if !ok {
		log.Printf("member `%v::%v` not found", c.Name, a.Name)
		return "<unknown-member>"
	}
	return m.Class
}
