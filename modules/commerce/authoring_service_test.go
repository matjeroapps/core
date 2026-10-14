package commerce

import (
	"strings"
	"testing"
)

func TestProductImageDescriptionUsesGeneratedDescriptionOrUserBrief(t *testing.T) {
	fields := []TextFieldResult{
		{FieldKey: FieldKeyName, Locale: LocaleEN, Value: "Blender"},
		{FieldKey: FieldKeyDescription, Locale: LocaleEN, Value: "1200 W blender with chopper and spice grinder"},
		{FieldKey: FieldKeyDescription, Locale: LocaleAR, Value: "خلاط 1200 واط مع كبة ومطحنة توابل"},
		{FieldKey: FieldKeyDescription, Locale: Locale("fr"), Value: "ignored unsupported locale"},
	}

	generated := productDescriptionFromFields(fields)
	if generated == "" {
		t.Fatal("expected generated descriptions")
	}
	if got := resolveProductImageDescription("user brief", generated); got != generated {
		t.Fatalf("generated description should take precedence: got %q", got)
	}
	if got := resolveProductImageDescription("  user supplied product description  ", " \n "); got != "user supplied product description" {
		t.Fatalf("user description should be the fallback, got %q", got)
	}
	if got := resolveProductImageDescription(" \n ", ""); got != "" {
		t.Fatalf("empty inputs should remain empty, got %q", got)
	}
	if got := productDescriptionFromFields(fields); got == "" || strings.Contains(got, "ignored unsupported locale") {
		t.Fatalf("description extraction should keep supported locales only: %q", got)
	}
}
