package commerce

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseTextProviderContentSnakeCaseFields(t *testing.T) {
	content := `{"fields":[{"field_key":"name","locale":"ar","value":"اسم مقترح"},{"field_key":"description","locale":"en","value":"Suggested description","attribute_code":"material"}]}`

	result, err := parseTextProviderContent(content)
	if err != nil {
		t.Fatalf("parseTextProviderContent() error = %v", err)
	}
	if len(result.Fields) != 2 {
		t.Fatalf("got %d fields, want 2", len(result.Fields))
	}
	if got := result.Fields[0]; got.FieldKey != FieldKeyName || got.Locale != LocaleAR || got.Value != "اسم مقترح" {
		t.Errorf("first field = %#v, want decoded Arabic name proposal", got)
	}
	if got := result.Fields[1]; got.FieldKey != FieldKeyDescription || got.Locale != LocaleEN || got.Value != "Suggested description" || got.AttributeCode != "material" {
		t.Errorf("second field = %#v, want decoded English description proposal", got)
	}

	encoded, err := json.Marshal(result.Fields[0])
	if err != nil {
		t.Fatalf("marshal field: %v", err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("unmarshal marshaled field: %v", err)
	}
	if roundTrip["field_key"] != "name" || roundTrip["locale"] != "ar" {
		t.Errorf("field JSON keys = %#v, want snake_case keys", roundTrip)
	}
}

func TestParseTextProviderContentRejectsNoProposals(t *testing.T) {
	if _, err := parseTextProviderContent(`{"fields":[]}`); err == nil {
		t.Fatal("parseTextProviderContent() accepted an empty proposal list")
	} else if got := failureCode(err); got != ProviderFailureInvalidOutput {
		t.Fatalf("failureCode() = %q, want %q", got, ProviderFailureInvalidOutput)
	}
}

func TestParseTextProviderContentAcceptsProposalArray(t *testing.T) {
	result, err := parseTextProviderContent(`[{"field_key":"name","locale":"ar","value":"اسم"}]`)
	if err != nil {
		t.Fatalf("parse direct proposal array: %v", err)
	}
	if len(result.Fields) != 1 || result.Fields[0].FieldKey != FieldKeyName || result.Fields[0].Locale != LocaleAR {
		t.Fatalf("parsed result = %#v, want one Arabic name proposal", result)
	}
}

func TestParseTextProviderContentRejectsMalformedOrUnsupportedProposals(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{name: "malformed JSON", content: `{"fields":`, wantErr: ProviderFailureInvalidOutput},
		{name: "unsupported field", content: `{"fields":[{"field_key":"price","locale":"en","value":"10"}]}`, wantErr: AuthoringUnsupportedField},
		{name: "unsupported locale", content: `{"fields":[{"field_key":"name","locale":"fr","value":"Nom"}]}`, wantErr: AuthoringUnsupportedField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseTextProviderContent(tt.content)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseTextProviderContent() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
