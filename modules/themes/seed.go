package themes

import (
	"context"
	"errors"
)

// Built-in platform theme registration foundation. Phase 4 ships exactly one
// platform-controlled default theme; the backend is already multi-theme capable
// (FREE/PREMIUM, multiple versions, marketplace-ready) without implementing the
// future Theme Marketplace here.

const (
	DefaultThemeKey                 = "matjero-default"
	DefaultThemeName                = "Matjero Default"
	DefaultThemeDescription         = "Platform-controlled default storefront theme."
	DefaultThemeVersion             = "1.0.0"
	DefaultComponentRegistryVersion = "1.0.0"

	BoutiqueThemeKey         = "matjero-boutique"
	BoutiqueThemeName        = "Matjero Boutique"
	BoutiqueThemeDescription = "Compact editorial luxury storefront theme."
	BoutiqueThemeVersion     = "1.0.0"
)

// DefaultConfigurationSchema is the JSON Schema that governs seller-customizable
// theme fields. It uses additionalProperties:false at every level so arbitrary
// unvalidated configuration JSON is rejected. It deliberately exposes only
// structured values (no raw HTML / CSS / JS editor).
var DefaultConfigurationSchema = map[string]any{
	"$schema":              "https://json-schema.org/draft/2020-12/schema",
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]any{
		"logo": map[string]any{
			"type":      "string",
			"maxLength": 512,
		},
		"favicon": map[string]any{
			"type":      "string",
			"maxLength": 512,
		},
		"colors": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"primary":    map[string]any{"type": "string", "pattern": "^#[0-9a-fA-F]{6}$"},
				"secondary":  map[string]any{"type": "string", "pattern": "^#[0-9a-fA-F]{6}$"},
				"background": map[string]any{"type": "string", "pattern": "^#[0-9a-fA-F]{6}$"},
				"text":       map[string]any{"type": "string", "pattern": "^#[0-9a-fA-F]{6}$"},
			},
		},
		"typography": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"font_family": map[string]any{"type": "string", "maxLength": 128},
				"base_size":   map[string]any{"type": "string", "enum": []any{"small", "medium", "large"}},
			},
		},
		"announcement_bar": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"enabled":          map[string]any{"type": "boolean"},
				"text":             map[string]any{"type": "string", "maxLength": 256},
				"background_color": map[string]any{"type": "string", "pattern": "^#[0-9a-fA-F]{6}$"},
				"text_color":       map[string]any{"type": "string", "pattern": "^#[0-9a-fA-F]{6}$"},
			},
		},
		"header": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"layout":      map[string]any{"type": "string", "enum": []any{"minimal", "centered", "classic"}},
				"show_search": map[string]any{"type": "boolean"},
			},
		},
		"footer": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"columns": map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
			},
		},
		"navigation": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"style": map[string]any{"type": "string", "enum": []any{"horizontal", "dropdown"}},
			},
		},
		"hero": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"title":     map[string]any{"type": "string", "maxLength": 128},
				"subtitle":  map[string]any{"type": "string", "maxLength": 256},
				"image_url": map[string]any{"type": "string", "maxLength": 1024},
				"cta_label": map[string]any{"type": "string", "maxLength": 64},
				"cta_url":   map[string]any{"type": "string", "maxLength": 1024},
			},
		},
		"homepage_sections": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"type"},
				"properties": map[string]any{
					"type":  map[string]any{"type": "string", "enum": []any{"featured", "category_grid", "product_carousel"}},
					"title": map[string]any{"type": "string", "maxLength": 128},
				},
			},
		},
		"product_card_layout": map[string]any{"type": "string", "enum": []any{"compact", "detailed"}},
		"category_layout":     map[string]any{"type": "string", "enum": []any{"grid", "list"}},
		"spacing":             map[string]any{"type": "string", "enum": []any{"comfortable", "compact"}},
	},
}

// DefaultConfiguration is the deterministic default configuration that ships with
// the built-in default theme. It is guaranteed to validate against
// DefaultConfigurationSchema (see TestDefaultConfigValidatesAgainstSchema).
var DefaultConfiguration = map[string]any{
	"logo":    "",
	"favicon": "",
	"colors": map[string]any{
		"primary":    "#0f766e",
		"secondary":  "#0d9488",
		"background": "#ffffff",
		"text":       "#0f172a",
	},
	"typography": map[string]any{
		"font_family": "Inter, system-ui, sans-serif",
		"base_size":   "medium",
	},
	"announcement_bar": map[string]any{
		"enabled":          false,
		"text":             "",
		"background_color": "#0f766e",
		"text_color":       "#ffffff",
	},
	"header": map[string]any{
		"layout":      "classic",
		"show_search": true,
	},
	"footer": map[string]any{
		"columns": 3,
	},
	"navigation": map[string]any{
		"style": "horizontal",
	},
	"hero": map[string]any{
		"title":     "",
		"subtitle":  "",
		"image_url": "",
		"cta_label": "",
		"cta_url":   "",
	},
	"homepage_sections": []any{
		map[string]any{"type": "featured", "title": "Featured Products"},
	},
	"product_card_layout": "detailed",
	"category_layout":     "grid",
	"spacing":             "comfortable",
}

// SeedBuiltInThemes idempotently creates the built-in default theme and its
// initial published version. It is safe to run on every startup/deploy: if the
// theme already exists it returns immediately without creating duplicates.
func (s Service) SeedBuiltInThemes(ctx context.Context) error {
	builtIn := []struct {
		key         string
		name        string
		description string
		version     string
	}{
		{DefaultThemeKey, DefaultThemeName, DefaultThemeDescription, DefaultThemeVersion},
		{BoutiqueThemeKey, BoutiqueThemeName, BoutiqueThemeDescription, BoutiqueThemeVersion},
	}

	for _, item := range builtIn {
		if _, err := s.repo.GetThemeByKey(ctx, item.key); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		theme, err := s.repo.CreateTheme(ctx, item.key, item.name, item.description, ThemeTypeFree, ThemeStatusActive)
		if err != nil {
			return err
		}
		if _, err := s.repo.CreateThemeVersion(ctx, theme.ID, item.version, ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, DefaultComponentRegistryVersion); err != nil {
			return err
		}
	}
	return nil
}
