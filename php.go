package main

import "mibk.dev/elph/resolved"

var phpSuperglobals = []string{
	"$_GET",
	"$_POST",
	"$_SERVER",
	"$_REQUEST",
	"$_SESSION",
	"$_COOKIE",
	"$_FILES",
	"$_ENV",
	"$GLOBALS",
	"$http_response_header",
}

var phpBuiltinFuncs = map[string]resolved.Type{
	"array_keys":     resolved.Array,
	"array_values":   resolved.Array,
	"array_unique":   resolved.Array,
	"array_reverse":  resolved.Array,
	"array_merge":    resolved.Array,
	"array_filter":   resolved.Array,
	"array_map":      resolved.Array,
	"array_slice":    resolved.Array,
	"array_chunk":    resolved.Array,
	"array_flip":     resolved.Array,
	"array_combine":  resolved.Array,
	"compact":        resolved.Array,
	"explode":        resolved.Array,
	"range":          resolved.Array,
	"count":          resolved.Int,
	"sizeof":         resolved.Int,
	"strlen":         resolved.Int,
	"preg_match":     resolved.Int,
	"preg_match_all": resolved.Int,
	"intval":         resolved.Int,
	"substr":         resolved.String,
	"strtolower":     resolved.String,
	"strtoupper":     resolved.String,
	"trim":           resolved.String,
	"ltrim":          resolved.String,
	"rtrim":          resolved.String,
	"sprintf":        resolved.String,
	"implode":        resolved.String,
	"json_encode":    resolved.String,
	"strval":         resolved.String,
	"floatval":       resolved.Float,
	"boolval":        resolved.Bool,
	"is_array":       resolved.Bool,
	"is_string":      resolved.Bool,
	"is_int":         resolved.Bool,
	"is_null":        resolved.Bool,
	"isset":          resolved.Bool,
	"empty":          resolved.Bool,
	"in_array":       resolved.Bool,
	"file_exists":    resolved.Bool,
	"class_exists":   resolved.Bool,
}

func (l *checker) applyByRefEffects(x *FuncCall) {
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
