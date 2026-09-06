package commerce

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Structured product page sections are stored as JSONB, but every accepted
// shape is validated here against typed rules — one validator per section
// type, each of which knows which fields are GLOBAL (shared across locales)
// and which are LOCALIZED. The validator is authoritative at save time and is
// re-applied inside the publish transaction; the legacy regex sweep remains
// only as defense-in-depth.

const (
	maxProductPageSections      = 20
	maxSectionPayloadBytes      = 16 * 1024
	maxSectionTotalPayloadBytes = 64 * 1024

	maxLocalizedHeadingChars = 200
	maxLocalizedBodyChars    = 5000
	maxHighlightItems        = 10
	maxHighlightItemChars    = 200
	maxSpecificationItems    = 30
	maxSpecificationKeyChars = 100
	maxSpecificationValChars = 500
	maxFAQItems              = 20
	maxFAQQuestionChars      = 300
	maxFAQAnswerChars        = 2000
	maxFinalCTABodyChars     = 1000
)

var productPageSectionTypes = map[string]bool{
	"description":    true,
	"highlights":     true,
	"image_text":     true,
	"specifications": true,
	"faq":            true,
	"final_cta":      true,
}

var imageTextLayouts = map[string]bool{"left": true, "right": true}

var approvedFinalCTAActions = map[string]bool{"add_to_cart": true, "buy_now": true}

// supportedLocales bounds the localized keys of every section.
var supportedLocales = map[string]bool{"en": true, "ar": true}

// ValidateProductPageSections validates the section envelope and each
// section's typed content. productMediaIDs is the set of media record IDs that
// belong to the same product; image_text sections must reference one of them.
// It returns one human-readable reason per defect so the same output can power
// authoring errors and publish readiness reasons.
func ValidateProductPageSections(sections []ProductPageSection, productMediaIDs map[string]bool) []string {
	var reasons []string

	if len(sections) > maxProductPageSections {
		return append(reasons, fmt.Sprintf("maximum %d sections allowed", maxProductPageSections))
	}

	totalBytes := 0
	seenIDs := map[string]bool{}
	for i, sec := range sections {
		label := fmt.Sprintf("section[%d]", i)
		if sec.ID == "" {
			reasons = append(reasons, label+": id is required")
		} else if seenIDs[sec.ID] {
			reasons = append(reasons, label+": duplicate section id "+sec.ID)
		}
		seenIDs[sec.ID] = true

		if sec.SortOrder < 0 {
			reasons = append(reasons, label+": sort_order must not be negative")
		}

		if !productPageSectionTypes[sec.Type] {
			reasons = append(reasons, label+": unknown section type "+sec.Type)
			continue
		}

		contentBytes, err := json.Marshal(sec.Content)
		if err != nil {
			reasons = append(reasons, label+": content is not valid JSON")
			continue
		}
		totalBytes += len(contentBytes)
		if len(contentBytes) > maxSectionPayloadBytes {
			reasons = append(reasons, label+": content exceeds payload limit")
		}

		switch sec.Type {
		case "description":
			reasons = append(reasons, validateDescriptionContent(sec.Content, label)...)
		case "highlights":
			reasons = append(reasons, validateHighlightsContent(sec.Content, label)...)
		case "image_text":
			reasons = append(reasons, validateImageTextContent(sec.Content, label, productMediaIDs)...)
		case "specifications":
			reasons = append(reasons, validateSpecificationsContent(sec.Content, label)...)
		case "faq":
			reasons = append(reasons, validateFAQContent(sec.Content, label)...)
		case "final_cta":
			reasons = append(reasons, validateFinalCTAContent(sec.Content, label)...)
		}
	}

	if totalBytes > maxSectionTotalPayloadBytes {
		reasons = append(reasons, "sections exceed total payload limit")
	}

	return reasons
}

// rejectUnknownKeys rejects any top-level content key that is not in the
// allowed set for the section type.
func rejectUnknownKeys(content map[string]any, allowed map[string]bool, label string) []string {
	var reasons []string
	for key := range content {
		if !allowed[key] {
			reasons = append(reasons, fmt.Sprintf("%s: unexpected field %s", label, key))
		}
	}
	return reasons
}

// localizedObjects extracts the en/ar localized objects from content and
// rejects non-locale keys. At least one localized entry must be present.
func localizedObjects(content map[string]any, label string) (map[string]map[string]any, []string) {
	var reasons []string
	out := map[string]map[string]any{}
	for key, val := range content {
		if !supportedLocales[key] {
			continue // unknown keys are reported by rejectUnknownKeys
		}
		obj, ok := val.(map[string]any)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("%s %s: content must be an object", label, key))
			continue
		}
		out[key] = obj
	}
	if len(out) == 0 {
		reasons = append(reasons, fmt.Sprintf("%s: at least one of en, ar localized content is required", label))
	}
	return out, reasons
}

func validateDescriptionContent(content map[string]any, label string) []string {
	reasons := rejectUnknownKeys(content, supportedLocales, label)
	loc, locReasons := localizedObjects(content, label)
	reasons = append(reasons, locReasons...)
	for locale, obj := range loc {
		if err := rejectLocalizedKeys(obj, []string{"heading", "body"}, label, locale); len(err) > 0 {
			reasons = append(reasons, err...)
			continue
		}
		hasHeading := nonEmptyString(obj, "heading")
		hasBody := nonEmptyString(obj, "body")
		if !hasHeading && !hasBody {
			reasons = append(reasons, fmt.Sprintf("%s %s: heading or body is required", label, locale))
		}
		reasons = append(reasons, boundedString(obj, "heading", maxLocalizedHeadingChars, false, label, locale)...)
		reasons = append(reasons, boundedString(obj, "body", maxLocalizedBodyChars, false, label, locale)...)
	}
	return reasons
}

func validateHighlightsContent(content map[string]any, label string) []string {
	reasons := rejectUnknownKeys(content, supportedLocales, label)
	loc, locReasons := localizedObjects(content, label)
	reasons = append(reasons, locReasons...)
	for locale, obj := range loc {
		if err := rejectLocalizedKeys(obj, []string{"title", "items"}, label, locale); len(err) > 0 {
			reasons = append(reasons, err...)
			continue
		}
		reasons = append(reasons, boundedString(obj, "title", maxLocalizedHeadingChars, false, label, locale)...)
		itemsRaw, ok := obj["items"].([]any)
		if !ok || len(itemsRaw) == 0 {
			reasons = append(reasons, fmt.Sprintf("%s %s: items are required", label, locale))
			continue
		}
		if len(itemsRaw) > maxHighlightItems {
			reasons = append(reasons, fmt.Sprintf("%s %s: maximum %d highlight items allowed", label, locale, maxHighlightItems))
		}
		for j, item := range itemsRaw {
			s, ok := item.(string)
			if !ok || strings.TrimSpace(s) == "" {
				reasons = append(reasons, fmt.Sprintf("%s %s: item %d must be a non-empty string", label, locale, j))
				continue
			}
			if len([]rune(s)) > maxHighlightItemChars {
				reasons = append(reasons, fmt.Sprintf("%s %s: item %d exceeds %d characters", label, locale, j, maxHighlightItemChars))
			}
		}
	}
	return reasons
}

func validateImageTextContent(content map[string]any, label string, productMediaIDs map[string]bool) []string {
	allowed := map[string]bool{"media_id": true, "layout": true, "en": true, "ar": true}
	reasons := rejectUnknownKeys(content, allowed, label)

	mediaID, ok := content["media_id"].(string)
	if !ok || strings.TrimSpace(mediaID) == "" {
		reasons = append(reasons, fmt.Sprintf("%s: media_id is required", label))
	} else if !productMediaIDs[mediaID] {
		reasons = append(reasons, fmt.Sprintf("%s: media_id does not reference media of the same product", label))
	}
	if layout, present := content["layout"]; present && layout != nil {
		l, ok := layout.(string)
		if !ok || !imageTextLayouts[l] {
			reasons = append(reasons, fmt.Sprintf("%s: layout must be one of left, right", label))
		}
	}

	loc, locReasons := localizedObjects(content, label)
	reasons = append(reasons, locReasons...)
	for locale, obj := range loc {
		if err := rejectLocalizedKeys(obj, []string{"heading", "body"}, label, locale); len(err) > 0 {
			reasons = append(reasons, err...)
			continue
		}
		// media_id is a global field; it must never be duplicated per locale.
		if _, dup := obj["media_id"]; dup {
			reasons = append(reasons, fmt.Sprintf("%s %s: media_id is a global field and must not be repeated per locale", label, locale))
		}
		reasons = append(reasons, boundedString(obj, "heading", maxLocalizedHeadingChars, false, label, locale)...)
		reasons = append(reasons, boundedString(obj, "body", maxLocalizedBodyChars, false, label, locale)...)
	}
	return reasons
}

func validateSpecificationsContent(content map[string]any, label string) []string {
	reasons := rejectUnknownKeys(content, supportedLocales, label)
	loc, locReasons := localizedObjects(content, label)
	reasons = append(reasons, locReasons...)
	for locale, obj := range loc {
		if err := rejectLocalizedKeys(obj, []string{"items"}, label, locale); len(err) > 0 {
			reasons = append(reasons, err...)
			continue
		}
		reasons = append(reasons, validateKVItems(obj, "items", maxSpecificationItems, []string{"key", "value"},
			maxSpecificationKeyChars, maxSpecificationValChars, label, locale)...)
	}
	return reasons
}

func validateFAQContent(content map[string]any, label string) []string {
	reasons := rejectUnknownKeys(content, supportedLocales, label)
	loc, locReasons := localizedObjects(content, label)
	reasons = append(reasons, locReasons...)
	for locale, obj := range loc {
		if err := rejectLocalizedKeys(obj, []string{"items"}, label, locale); len(err) > 0 {
			reasons = append(reasons, err...)
			continue
		}
		reasons = append(reasons, validateKVItems(obj, "items", maxFAQItems, []string{"question", "answer"},
			maxFAQQuestionChars, maxFAQAnswerChars, label, locale)...)
	}
	return reasons
}

func validateFinalCTAContent(content map[string]any, label string) []string {
	allowed := map[string]bool{"action": true, "en": true, "ar": true}
	reasons := rejectUnknownKeys(content, allowed, label)

	// The purchase action is global: one approved action for the section.
	action, ok := content["action"].(string)
	if !ok || !approvedFinalCTAActions[action] {
		reasons = append(reasons, fmt.Sprintf("%s: action must be one of add_to_cart, buy_now", label))
	}

	loc, locReasons := localizedObjects(content, label)
	reasons = append(reasons, locReasons...)
	for locale, obj := range loc {
		if err := rejectLocalizedKeys(obj, []string{"title", "body"}, label, locale); len(err) > 0 {
			reasons = append(reasons, err...)
			continue
		}
		// No arbitrary destinations: any link-like field inside a locale is
		// rejected by the unknown-key rule above; the action itself is global.
		if _, dup := obj["action"]; dup {
			reasons = append(reasons, fmt.Sprintf("%s %s: action is a global field and must not be repeated per locale", label, locale))
		}
		reasons = append(reasons, boundedString(obj, "title", maxLocalizedHeadingChars, true, label, locale)...)
		reasons = append(reasons, boundedString(obj, "body", maxFinalCTABodyChars, false, label, locale)...)
	}
	return reasons
}

// rejectLocalizedKeys rejects unexpected fields inside one locale object.
func rejectLocalizedKeys(obj map[string]any, allowedFields []string, label, locale string) []string {
	allowed := map[string]bool{}
	for _, f := range allowedFields {
		allowed[f] = true
	}
	var reasons []string
	for key := range obj {
		if !allowed[key] {
			reasons = append(reasons, fmt.Sprintf("%s %s: unexpected field %s", label, locale, key))
		}
	}
	return reasons
}

// validateKVItems validates a localized {items: [{k, v}]} list where the item
// field names and bounds are supplied by the section type.
func validateKVItems(obj map[string]any, field string, maxItems int, itemFields []string, maxKeyChars, maxValChars int, label, locale string) []string {
	var reasons []string
	itemsRaw, ok := obj[field].([]any)
	if !ok || len(itemsRaw) == 0 {
		reasons = append(reasons, fmt.Sprintf("%s %s: items are required", label, locale))
		return reasons
	}
	if len(itemsRaw) > maxItems {
		reasons = append(reasons, fmt.Sprintf("%s %s: maximum %d items allowed", label, locale, maxItems))
	}
	for j, item := range itemsRaw {
		entry, ok := item.(map[string]any)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("%s %s: item %d must be an object", label, locale, j))
			continue
		}
		itemLabel := fmt.Sprintf("%s %s item %d", label, locale, j)
		reasons = append(reasons, rejectLocalizedKeys(entry, itemFields, itemLabel, "")...)
		reasons = append(reasons, boundedString(entry, itemFields[0], maxKeyChars, true, itemLabel, "")...)
		reasons = append(reasons, boundedString(entry, itemFields[1], maxValChars, true, itemLabel, "")...)
	}
	return reasons
}

func boundedString(obj map[string]any, field string, maxChars int, required bool, label, locale string) []string {
	raw, ok := obj[field]
	if !ok || raw == nil {
		if required {
			return []string{fmt.Sprintf("%s %s: %s is required", label, locale, field)}
		}
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return []string{fmt.Sprintf("%s %s: %s must be a string", label, locale, field)}
	}
	if required && strings.TrimSpace(s) == "" {
		return []string{fmt.Sprintf("%s %s: %s must not be empty", label, locale, field)}
	}
	if len([]rune(s)) > maxChars {
		return []string{fmt.Sprintf("%s %s: %s exceeds %d characters", label, locale, field, maxChars)}
	}
	return nil
}

func nonEmptyString(obj map[string]any, field string) bool {
	s, ok := obj[field].(string)
	return ok && strings.TrimSpace(s) != ""
}
