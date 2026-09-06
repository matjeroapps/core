package commerce

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Structured product page sections are stored as JSONB, but every accepted
// shape is validated here against typed rules. The validator drives both
// authoring errors (UpdateListingPresentationForSubject) and publish
// readiness, so a section that could never render correctly also cannot make
// a product publishable.

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

		loc, locReasons := localizedSectionContent(sec.Content)
		if len(locReasons) > 0 {
			for _, r := range locReasons {
				reasons = append(reasons, label+": "+r)
			}
			continue
		}

		switch sec.Type {
		case "description":
			reasons = append(reasons, validateDescription(loc, label)...)
		case "highlights":
			reasons = append(reasons, validateHighlights(loc, label)...)
		case "image_text":
			reasons = append(reasons, validateImageText(loc, label, productMediaIDs)...)
		case "specifications":
			reasons = append(reasons, validateSpecifications(loc, label)...)
		case "faq":
			reasons = append(reasons, validateFAQ(loc, label)...)
		case "final_cta":
			reasons = append(reasons, validateFinalCTA(loc, label)...)
		}
	}

	if totalBytes > maxSectionTotalPayloadBytes {
		reasons = append(reasons, "sections exceed total payload limit")
	}

	return reasons
}

// localizedSectionContent requires content to be a map of supported locale
// codes to objects, with at least one localized entry.
func localizedSectionContent(content map[string]any) (map[string]map[string]any, []string) {
	if len(content) == 0 {
		return nil, []string{"content is required"}
	}
	supportedLocales := map[string]bool{"en": true, "ar": true}
	out := map[string]map[string]any{}
	var reasons []string
	for key, val := range content {
		if !supportedLocales[key] {
			reasons = append(reasons, "unsupported locale key "+key)
			continue
		}
		obj, ok := val.(map[string]any)
		if !ok {
			reasons = append(reasons, "locale "+key+" content must be an object")
			continue
		}
		out[key] = obj
	}
	if len(out) == 0 && len(reasons) > 0 {
		return nil, reasons
	}
	return out, reasons
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

func validateDescription(loc map[string]map[string]any, label string) []string {
	var reasons []string
	for locale, obj := range loc {
		hasHeading := boundedString(obj, "heading", maxLocalizedHeadingChars, false, label, locale) == nil && nonEmptyString(obj, "heading")
		hasBody := nonEmptyString(obj, "body")
		if !hasHeading && !hasBody {
			reasons = append(reasons, fmt.Sprintf("%s %s: heading or body is required", label, locale))
		}
		reasons = append(reasons, boundedString(obj, "heading", maxLocalizedHeadingChars, false, label, locale)...)
		reasons = append(reasons, boundedString(obj, "body", maxLocalizedBodyChars, false, label, locale)...)
	}
	return reasons
}

func nonEmptyString(obj map[string]any, field string) bool {
	s, ok := obj[field].(string)
	return ok && strings.TrimSpace(s) != ""
}

func validateHighlights(loc map[string]map[string]any, label string) []string {
	var reasons []string
	for locale, obj := range loc {
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

func validateImageText(loc map[string]map[string]any, label string, productMediaIDs map[string]bool) []string {
	var reasons []string
	for locale, obj := range loc {
		mediaID, ok := obj["media_id"].(string)
		if !ok || strings.TrimSpace(mediaID) == "" {
			reasons = append(reasons, fmt.Sprintf("%s %s: media_id is required", label, locale))
		} else if !productMediaIDs[mediaID] {
			reasons = append(reasons, fmt.Sprintf("%s %s: media_id does not reference media of the same product", label, locale))
		}
		reasons = append(reasons, boundedString(obj, "heading", maxLocalizedHeadingChars, false, label, locale)...)
		reasons = append(reasons, boundedString(obj, "body", maxLocalizedBodyChars, false, label, locale)...)
		if layout, ok := obj["layout"].(string); ok && layout != "" && !imageTextLayouts[layout] {
			reasons = append(reasons, fmt.Sprintf("%s %s: layout must be one of left, right", label, locale))
		}
	}
	return reasons
}

func validateSpecifications(loc map[string]map[string]any, label string) []string {
	var reasons []string
	for locale, obj := range loc {
		itemsRaw, ok := obj["items"].([]any)
		if !ok || len(itemsRaw) == 0 {
			reasons = append(reasons, fmt.Sprintf("%s %s: items are required", label, locale))
			continue
		}
		if len(itemsRaw) > maxSpecificationItems {
			reasons = append(reasons, fmt.Sprintf("%s %s: maximum %d specification items allowed", label, locale, maxSpecificationItems))
		}
		for j, item := range itemsRaw {
			entry, ok := item.(map[string]any)
			if !ok {
				reasons = append(reasons, fmt.Sprintf("%s %s: item %d must be an object with key/value", label, locale, j))
				continue
			}
			reasons = append(reasons, boundedString(entry, "key", maxSpecificationKeyChars, true, fmt.Sprintf("%s %s item %d", label, locale, j), "")...)
			reasons = append(reasons, boundedString(entry, "value", maxSpecificationValChars, true, fmt.Sprintf("%s %s item %d", label, locale, j), "")...)
		}
	}
	return reasons
}

func validateFAQ(loc map[string]map[string]any, label string) []string {
	var reasons []string
	for locale, obj := range loc {
		itemsRaw, ok := obj["items"].([]any)
		if !ok || len(itemsRaw) == 0 {
			reasons = append(reasons, fmt.Sprintf("%s %s: items are required", label, locale))
			continue
		}
		if len(itemsRaw) > maxFAQItems {
			reasons = append(reasons, fmt.Sprintf("%s %s: maximum %d FAQ items allowed", label, locale, maxFAQItems))
		}
		for j, item := range itemsRaw {
			entry, ok := item.(map[string]any)
			if !ok {
				reasons = append(reasons, fmt.Sprintf("%s %s: item %d must be an object with question/answer", label, locale, j))
				continue
			}
			reasons = append(reasons, boundedString(entry, "question", maxFAQQuestionChars, true, fmt.Sprintf("%s %s item %d", label, locale, j), "")...)
			reasons = append(reasons, boundedString(entry, "answer", maxFAQAnswerChars, true, fmt.Sprintf("%s %s item %d", label, locale, j), "")...)
		}
	}
	return reasons
}

func validateFinalCTA(loc map[string]map[string]any, label string) []string {
	var reasons []string
	for locale, obj := range loc {
		for key := range obj {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "url") || strings.Contains(lower, "href") || strings.Contains(lower, "link") {
				reasons = append(reasons, fmt.Sprintf("%s %s: field %s is not allowed; purchase actions only", label, locale, key))
			}
		}
		reasons = append(reasons, boundedString(obj, "title", maxLocalizedHeadingChars, true, label, locale)...)
		reasons = append(reasons, boundedString(obj, "body", maxFinalCTABodyChars, false, label, locale)...)
		action, ok := obj["action"].(string)
		if !ok || !approvedFinalCTAActions[action] {
			reasons = append(reasons, fmt.Sprintf("%s %s: action must be one of add_to_cart, buy_now", label, locale))
		}
	}
	return reasons
}
