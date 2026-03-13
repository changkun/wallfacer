package sandbox

import "strings"

type Type string

const (
	Claude Type = "claude"
	Codex  Type = "codex"
)

var all = []Type{Claude, Codex}

func All() []Type {
	out := make([]Type, len(all))
	copy(out, all)
	return out
}

func Parse(value string) (Type, bool) {
	switch Type(strings.ToLower(strings.TrimSpace(value))) {
	case Claude:
		return Claude, true
	case Codex:
		return Codex, true
	default:
		return "", false
	}
}

func Normalize(value string) Type {
	if parsed, ok := Parse(value); ok {
		return parsed
	}
	return Type(strings.ToLower(strings.TrimSpace(value)))
}

func Default(value string) Type {
	if parsed, ok := Parse(value); ok {
		return parsed
	}
	return Claude
}

func (t Type) IsValid() bool {
	_, ok := Parse(string(t))
	return ok
}

func (t Type) OrDefault() Type {
	if t.IsValid() {
		return t
	}
	return Claude
}
