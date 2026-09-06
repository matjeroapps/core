package commerce

import (
	"strings"
	"testing"
)

var validMediaIDs = map[string]bool{"m-1": true, "m-2": true}

func reasonsJoined(reasons []string) string {
	return strings.Join(reasons, "; ")
}

func TestValidateDescriptionSections(t *testing.T) {
	valid := []ProductPageSection{{
		ID: "s1", Type: "description", Enabled: true, SortOrder: 10,
		Content: map[string]any{
			"en": map[string]any{"heading": "Description", "body": "Body"},
			"ar": map[string]any{"heading": "الوصف", "body": "نص"},
		},
	}}
	if reasons := ValidateProductPageSections(valid, validMediaIDs); len(reasons) != 0 {
		t.Fatalf("valid description rejected: %s", reasonsJoined(reasons))
	}

	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "description", Content: map[string]any{"de": map[string]any{"heading": "X"}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("unknown locale key must be rejected")
	}

	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "description", Content: map[string]any{"en": map[string]any{"heading": "H", "body": strings.Repeat("x", 5001)}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("oversized body must be rejected")
	}

	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "description", Content: map[string]any{"en": map[string]any{"heading": "H", "url": "https://x"}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("unknown field must be rejected")
	}
}

func TestValidateHighlightsSections(t *testing.T) {
	valid := []ProductPageSection{{
		ID: "s1", Type: "highlights", Enabled: true, SortOrder: 20,
		Content: map[string]any{
			"en": map[string]any{"title": "Highlights", "items": []any{"One", "Two"}},
			"ar": map[string]any{"title": "المميزات", "items": []any{"ميزة"}},
		},
	}}
	if reasons := ValidateProductPageSections(valid, validMediaIDs); len(reasons) != 0 {
		t.Fatalf("valid highlights rejected: %s", reasonsJoined(reasons))
	}

	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "highlights", Content: map[string]any{"en": map[string]any{"title": "H", "items": []any{}}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("empty items must be rejected")
	}

	over := map[string]any{"en": map[string]any{"items": []any{}}}
	itemList := make([]any, maxHighlightItems+1)
	for i := range itemList {
		itemList[i] = "item"
	}
	over["en"] = map[string]any{"items": itemList}
	if reasons := ValidateProductPageSections([]ProductPageSection{{ID: "s1", Type: "highlights", Content: over}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("too many highlight items must be rejected")
	}
}

func TestValidateImageTextSections(t *testing.T) {
	valid := []ProductPageSection{{
		ID: "s1", Type: "image_text", Enabled: true, SortOrder: 30,
		Content: map[string]any{
			"media_id": "m-1",
			"layout":   "left",
			"en":       map[string]any{"heading": "Quality", "body": "B"},
			"ar":       map[string]any{"heading": "الجودة", "body": "نص"},
		},
	}}
	if reasons := ValidateProductPageSections(valid, validMediaIDs); len(reasons) != 0 {
		t.Fatalf("valid image_text rejected: %s", reasonsJoined(reasons))
	}

	// media_id is global and required.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "image_text",
		Content: map[string]any{"layout": "left", "en": map[string]any{"heading": "H"}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("missing global media_id must be rejected")
	}

	// Cross-product media reference.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "image_text",
		Content: map[string]any{"media_id": "other-product-media", "layout": "left", "en": map[string]any{"heading": "H"}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("cross-product media_id must be rejected")
	}

	// Invalid layout.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "image_text",
		Content: map[string]any{"media_id": "m-1", "layout": "center", "en": map[string]any{"heading": "H"}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("invalid layout must be rejected")
	}

	// Per-locale media_id duplication must be rejected.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "image_text",
		Content: map[string]any{
			"media_id": "m-1", "layout": "left",
			"en": map[string]any{"heading": "H", "media_id": "m-1"},
		},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("per-locale media_id must be rejected")
	}
}

func TestValidateSpecificationsSections(t *testing.T) {
	valid := []ProductPageSection{{
		ID: "s1", Type: "specifications", Enabled: true, SortOrder: 40,
		Content: map[string]any{
			"en": map[string]any{"items": []any{map[string]any{"key": "Material", "value": "Cotton"}}},
			"ar": map[string]any{"items": []any{map[string]any{"key": "الخامة", "value": "قطن"}}},
		},
	}}
	if reasons := ValidateProductPageSections(valid, validMediaIDs); len(reasons) != 0 {
		t.Fatalf("valid specifications rejected: %s", reasonsJoined(reasons))
	}

	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "specifications",
		Content: map[string]any{"en": map[string]any{"items": []any{map[string]any{"key": "K", "value": "V", "extra": 1}}}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("unknown item field must be rejected")
	}
}

func TestValidateFAQSections(t *testing.T) {
	valid := []ProductPageSection{{
		ID: "s1", Type: "faq", Enabled: true, SortOrder: 50,
		Content: map[string]any{
			"en": map[string]any{"items": []any{map[string]any{"question": "Q?", "answer": "A."}}},
			"ar": map[string]any{"items": []any{map[string]any{"question": "السؤال؟", "answer": "الإجابة."}}},
		},
	}}
	if reasons := ValidateProductPageSections(valid, validMediaIDs); len(reasons) != 0 {
		t.Fatalf("valid faq rejected: %s", reasonsJoined(reasons))
	}

	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "faq",
		Content: map[string]any{"en": map[string]any{"items": []any{map[string]any{"question": "Q?"}}}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("missing answer must be rejected")
	}
}

func TestValidateFinalCTASections(t *testing.T) {
	for _, action := range []string{"add_to_cart", "buy_now"} {
		valid := []ProductPageSection{{
			ID: "s1", Type: "final_cta", Enabled: true, SortOrder: 60,
			Content: map[string]any{
				"action": action,
				"en":     map[string]any{"title": "Ready to order?", "body": "B"},
				"ar":     map[string]any{"title": "جاهز للطلب؟", "body": "نص"},
			},
		}}
		if reasons := ValidateProductPageSections(valid, validMediaIDs); len(reasons) != 0 {
			t.Fatalf("valid final_cta (%s) rejected: %s", action, reasonsJoined(reasons))
		}
	}

	// action is global and required.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "final_cta",
		Content: map[string]any{"en": map[string]any{"title": "T"}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("missing global action must be rejected")
	}

	// Unknown action.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "final_cta",
		Content: map[string]any{"action": "custom_thing", "en": map[string]any{"title": "T"}},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("unknown action must be rejected")
	}

	// Per-locale action duplication rejected.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "final_cta",
		Content: map[string]any{
			"action": "add_to_cart",
			"en":     map[string]any{"title": "T", "action": "add_to_cart"},
		},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("per-locale action must be rejected")
	}

	// Any URL-ish destination rejected.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "final_cta",
		Content: map[string]any{
			"action": "add_to_cart", "url": "https://external.example",
			"en": map[string]any{"title": "T"},
		},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("url field must be rejected")
	}
}

func TestValidateSectionEnvelope(t *testing.T) {
	// Unknown section type.
	if reasons := ValidateProductPageSections([]ProductPageSection{{
		ID: "s1", Type: "video", Content: map[string]any{},
	}}, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("unknown section type must be rejected")
	}

	// Duplicate ids.
	dup := []ProductPageSection{
		{ID: "s1", Type: "description", Content: map[string]any{"en": map[string]any{"heading": "H"}}},
		{ID: "s1", Type: "description", Content: map[string]any{"en": map[string]any{"heading": "H"}}},
	}
	if reasons := ValidateProductPageSections(dup, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("duplicate section ids must be rejected")
	}

	// More than the section cap.
	many := make([]ProductPageSection, maxProductPageSections+1)
	for i := range many {
		many[i] = ProductPageSection{ID: "s", Type: "description", Content: map[string]any{"en": map[string]any{"heading": "H"}}}
	}
	if reasons := ValidateProductPageSections(many, validMediaIDs); len(reasons) == 0 {
		t.Fatalf("over-cap sections must be rejected")
	}
}
