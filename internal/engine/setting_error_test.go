package engine_test

import (
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestSettingErrorDetails(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, rule, option string
		value              any
		element            int
	}{
		{"unknown rule", "MD999", "", nil, -1},
		{"unknown option", "MD009", "missing", true, -1},
		{"wrong kind", "MD009", "hard_break_spaces", "two", -1},
		{"numeric domain", "MD009", "hard_break_spaces", -1, -1},
		{"list kind", "MD054", "allowed_styles", []any{"inline"}, -1},
		{"list length", "MD054", "allowed_styles", []string{}, -1},
		{"list choice", "MD054", "allowed_styles", []string{"inline", "bad"}, 1},
		{"HTML marker", "MD073", "start_marker", "<!-- missing", -1},
		{"scalar syntax", "MD084", "allowed_codepoints", []string{"FEFF", "U+20"}, 1},
		{"scalar range", "MD084", "allowed_codepoints", []string{"FEFF", "D800"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := engine.Options{}
			if tt.option != "" {
				options[tt.option] = tt.value
			}
			_, err := r.ResolveSettings(engine.Settings{tt.rule: {Options: options}}, engine.ValidationProviders{})
			assertSettingError(t, err, tt.rule, tt.option, tt.element)
		})
	}
	for _, tt := range []struct {
		option  string
		options engine.Options
	}{
		{"min_level", engine.Options{"min_level": 5, "max_level": 4}},
		{"end_marker", engine.Options{"start_marker": "<!-- same -->", "end_marker": "<!-- same -->"}},
	} {
		_, err := r.ResolveSettings(engine.Settings{"MD073": {Options: tt.options}}, engine.ValidationProviders{})
		assertSettingError(t, err, "MD073", tt.option, -1)
	}
}

func TestSettingErrorPreservesHostnameCause(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("rejected host")
	settings := engine.Settings{"MD095": {Options: engine.Options{"allowed_hosts": []string{"example.com", "bad"}}}}
	_, err = r.ResolveSettings(settings, engine.ValidationProviders{ValidateHostname: func(host string) error {
		if host == "bad" {
			return sentinel
		}
		return nil
	}})
	assertSettingError(t, err, "MD095", "allowed_hosts", 1)
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost provider cause: %v", err)
	}
	_, err = r.ResolveSettings(settings, engine.ValidationProviders{})
	assertSettingError(t, err, "MD095", "allowed_hosts", 0)
	if !errors.Is(err, engine.ErrHostnameValidationUnavailable) {
		t.Fatalf("lost unavailable cause: %v", err)
	}
}

func TestSettingFromMappingErrorDetails(t *testing.T) {
	for _, key := range []string{"enabled", "allow_directives"} {
		_, err := engine.SettingFromMapping(map[string]any{key: "yes"})
		assertSettingError(t, err, "", key, -1)
	}
}

func assertSettingError(t *testing.T, err error, rule, option string, element int) {
	t.Helper()
	var detail *engine.SettingError
	if !errors.As(err, &detail) {
		t.Fatalf("expected SettingError, got %T: %v", err, err)
	}
	if detail.RuleID != rule || detail.Option != option {
		t.Errorf("location = (%q, %q), want (%q, %q)", detail.RuleID, detail.Option, rule, option)
	}
	if element < 0 {
		if detail.Element != nil {
			t.Errorf("unexpected element %d", *detail.Element)
		}
	} else if detail.Element == nil || *detail.Element != element {
		t.Errorf("element = %v, want %d", detail.Element, element)
	}
	if detail.Err == nil || detail.Error() == "" {
		t.Error("missing validation error")
	}
}
