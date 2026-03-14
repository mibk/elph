package main

import "mibk.dev/elph/resolved"

var phpSuperglobals = map[string]bool{
	"$_GET":                 true,
	"$_POST":                true,
	"$_SERVER":              true,
	"$_REQUEST":             true,
	"$_SESSION":             true,
	"$_COOKIE":              true,
	"$_FILES":               true,
	"$_ENV":                 true,
	"$GLOBALS":              true,
	"$http_response_header": true,
}

var phpBuiltinFuncs = map[string]resolved.Type{
	"array_keys":     resolved.TypeFromName("array"),
	"array_values":   resolved.TypeFromName("array"),
	"array_unique":   resolved.TypeFromName("array"),
	"array_reverse":  resolved.TypeFromName("array"),
	"array_merge":    resolved.TypeFromName("array"),
	"array_filter":   resolved.TypeFromName("array"),
	"array_map":      resolved.TypeFromName("array"),
	"array_slice":    resolved.TypeFromName("array"),
	"array_chunk":    resolved.TypeFromName("array"),
	"array_flip":     resolved.TypeFromName("array"),
	"array_combine":  resolved.TypeFromName("array"),
	"compact":        resolved.TypeFromName("array"),
	"explode":        resolved.TypeFromName("array"),
	"range":          resolved.TypeFromName("array"),
	"count":          resolved.TypeFromName("int"),
	"sizeof":         resolved.TypeFromName("int"),
	"strlen":         resolved.TypeFromName("int"),
	"preg_match":     resolved.TypeFromName("int"),
	"preg_match_all": resolved.TypeFromName("int"),
	"intval":         resolved.TypeFromName("int"),
	"substr":         resolved.TypeFromName("string"),
	"strtolower":     resolved.TypeFromName("string"),
	"strtoupper":     resolved.TypeFromName("string"),
	"trim":           resolved.TypeFromName("string"),
	"ltrim":          resolved.TypeFromName("string"),
	"rtrim":          resolved.TypeFromName("string"),
	"sprintf":        resolved.TypeFromName("string"),
	"implode":        resolved.TypeFromName("string"),
	"json_encode":    resolved.TypeFromName("string"),
	"strval":         resolved.TypeFromName("string"),
	"floatval":       resolved.TypeFromName("float"),
	"boolval":        resolved.TypeFromName("bool"),
	"is_array":       resolved.TypeFromName("bool"),
	"is_string":      resolved.TypeFromName("bool"),
	"is_int":         resolved.TypeFromName("bool"),
	"is_null":        resolved.TypeFromName("bool"),
	"isset":          resolved.TypeFromName("bool"),
	"empty":          resolved.TypeFromName("bool"),
	"in_array":       resolved.TypeFromName("bool"),
	"file_exists":    resolved.TypeFromName("bool"),
	"class_exists":   resolved.TypeFromName("bool"),
}

func (l *linter) applyByRefEffects(x *FuncCall) {
	switch x.Name {
	case "preg_match", "preg_match_all":
		// 3rd argument ($matches) is passed by reference as array.
		if x.Args == nil || len(x.Args.Stmts) < 3 {
			return
		}
		nodes := x.Args.Stmts[2].Nodes
		if len(nodes) == 1 {
			if v, ok := nodes[0].(*VarExpr); ok {
				l.scope[v.Name] = resolved.Array
			}
		}
	}
}
