package commerce

import (
	"strings"
	"time"
)

type AuthoringBranch string

const (
	AuthoringBranchText  AuthoringBranch = "text"
	AuthoringBranchImage AuthoringBranch = "image"
)

type AuthoringProviderConfig struct {
	TextProvider    string
	TextModel       string
	TextAPIBaseURL  string
	TextAPIKey      string
	ImageProvider   string
	ImageModel      string
	ImageAPIBaseURL string
	ImageAPIKey     string
	RequestTimeout  time.Duration
}

type AuthoringProviderRegistry struct {
	TextGenerator TextGenerator
	ImageComposer ImageComposer
	TextFailure   string
	ImageFailure  string
}

func NewAuthoringProviderRegistry(cfg AuthoringProviderConfig) AuthoringProviderRegistry {
	reg := AuthoringProviderRegistry{}
	if gen, code := selectTextGenerator(cfg); code != "" {
		reg.TextFailure = code
	} else {
		reg.TextGenerator = gen
	}
	if composer, code := selectImageComposer(cfg); code != "" {
		reg.ImageFailure = code
	} else {
		reg.ImageComposer = composer
	}
	return reg
}

func selectTextGenerator(cfg AuthoringProviderConfig) (TextGenerator, string) {
	provider := strings.TrimSpace(cfg.TextProvider)
	if provider == "" || strings.TrimSpace(cfg.TextModel) == "" || strings.TrimSpace(cfg.TextAPIBaseURL) == "" || strings.TrimSpace(cfg.TextAPIKey) == "" {
		return nil, ProviderFailureNotConfigured
	}
	if provider != "openai" {
		if provider == "openai.image-composite" {
			return nil, ProviderFailureCapabilityUnsupported
		}
		return nil, ProviderFailureUnsupported
	}
	return NewOpenAITextProvider(cfg.TextAPIBaseURL, cfg.TextAPIKey, cfg.TextModel, cfg.RequestTimeout), ""
}

func selectImageComposer(cfg AuthoringProviderConfig) (ImageComposer, string) {
	provider := strings.TrimSpace(cfg.ImageProvider)
	if provider == "" || strings.TrimSpace(cfg.ImageModel) == "" || strings.TrimSpace(cfg.ImageAPIBaseURL) == "" || strings.TrimSpace(cfg.ImageAPIKey) == "" {
		return nil, ProviderFailureNotConfigured
	}
	if provider != "openai" {
		if provider == "openai.text" {
			return nil, ProviderFailureCapabilityUnsupported
		}
		return nil, ProviderFailureUnsupported
	}
	return NewOpenAIImageComposer(cfg.ImageAPIBaseURL, cfg.ImageAPIKey, cfg.ImageModel, cfg.RequestTimeout), ""
}
