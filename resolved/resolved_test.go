package resolved

import "testing"

func TestTypeFromName(t *testing.T) {
	tests := []struct {
		name string
		want *Builtin
	}{
		{"double", Float},
		{"integer", Int},
		{"boolean", Bool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TypeFromName(tt.name)
			if got != tt.want {
				t.Errorf("TypeFromName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsBuiltinName(t *testing.T) {
	for _, name := range []string{"double", "integer", "boolean"} {
		if !IsBuiltinName(name) {
			t.Errorf("IsBuiltinName(%q) = false, want true", name)
		}
	}
	if IsBuiltinName(`App\Foo`) {
		t.Error(`IsBuiltinName("App\\Foo") = true, want false`)
	}
}
