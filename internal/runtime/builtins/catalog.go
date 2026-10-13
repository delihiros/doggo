package builtins

import (
	"fmt"
	"sort"

	"doggo/internal/runtime/profile"
)

type Builtin struct {
	Name string
}

type Catalog struct {
	entries map[string]*Builtin
}

var pureCoreBuiltinNames = []string{
	"*",
	"+",
	"-",
	"/",
	"<",
	"<=",
	"=",
	">",
	">=",
	"contains?",
	"count",
	"dec",
	"empty?",
	"first",
	"reduce",
	"map",
	"map?",
	"filter",
	"identity",
	"inc",
	"ifn?",
	"keyword?",
	"list?",
	"range",
	"last",
	"nil?",
	"not",
	"nth",
	"number?",
	"rest",
	"seq?",
	"set?",
	"some?",
	"string?",
	"symbol?",
	"take",
	"vector?",
}

var publicHostEffectBuiltinNames = []string{
	"slurp",
	"spit",
	"print",
	"println",
	"prn",
}

func FromProfile(p *profile.Profile) *Catalog {
	cat := &Catalog{entries: map[string]*Builtin{}}
	if p == nil {
		return cat
	}

	for _, feature := range p.Features() {
		switch feature {
		case profile.FeaturePureCore:
			for _, name := range pureCoreBuiltinNames {
				cat.entries[name] = &Builtin{Name: name}
			}
		case profile.FeatureFileRead:
			cat.entries["slurp"] = &Builtin{Name: "slurp"}
		case profile.FeatureHostEffects:
			for _, name := range publicHostEffectBuiltinNames {
				cat.entries[name] = &Builtin{Name: name}
			}
		}
	}

	return cat
}

func NamesForProfile(p *profile.Profile) []string {
	return FromProfile(p).Names()
}

func ValidateProfile(p *profile.Profile) error {
	if p == nil {
		return nil
	}
	for _, feature := range p.Features() {
		switch feature {
		case profile.FeaturePureCore, profile.FeatureFileRead, profile.FeatureHostEffects:
		default:
			return fmt.Errorf("unknown runtime profile feature %q", feature)
		}
	}
	return nil
}

func (c *Catalog) Lookup(name string) *Builtin {
	if c == nil {
		return nil
	}
	return c.entries[name]
}

func (c *Catalog) Names() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.entries))
	for name := range c.entries {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
