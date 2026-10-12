package commerce

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The adapter ids are custom to OpenAI and never generic: registering another
// provider value must produce a classified failure, never a guessed fallback.
func TestAuthoringRegistryClassifiesUnsupportedProviders(t *testing.T) {
	base := AuthoringProviderConfig{TextModel: "m", TextAPIBaseURL: "https://api.example", TextAPIKey: "k", ImageModel: "m2", ImageAPIBaseURL: "https://api.example", ImageAPIKey: "k2", RequestTimeout: 5 * time.Second}

	textCfg := base
	textCfg.TextProvider = "azure"
	if _, code := selectTextGenerator(textCfg); code != ProviderFailureUnsupported {
		t.Fatalf("text provider azure code = %q, want %q", code, ProviderFailureUnsupported)
	}
	imageCfg := base
	imageCfg.ImageProvider = "stability"
	if _, code := selectImageComposer(imageCfg); code != ProviderFailureUnsupported {
		t.Fatalf("image provider stability code = %q, want %q", code, ProviderFailureUnsupported)
	}

	// Adapter ids are never interchangeable: openai.text cannot serve the
	// image branch and openai.image-composite cannot serve the text branch.
	textCfg = base
	textCfg.TextProvider = "openai.image-composite"
	if _, code := selectTextGenerator(textCfg); code != ProviderFailureCapabilityUnsupported {
		t.Fatalf("text provider openai.image-composite code = %q, want %q", code, ProviderFailureCapabilityUnsupported)
	}
	imageCfg = base
	imageCfg.ImageProvider = "openai.text"
	if _, code := selectImageComposer(imageCfg); code != ProviderFailureCapabilityUnsupported {
		t.Fatalf("image provider openai.text code = %q, want %q", code, ProviderFailureCapabilityUnsupported)
	}
}

// Missing configuration is a classified failure, never a silent success and
// never a default provider/model choice.
func TestAuthoringRegistryMissingConfiguration(t *testing.T) {
	if _, code := selectTextGenerator(AuthoringProviderConfig{TextProvider: "openai"}); code != ProviderFailureNotConfigured {
		t.Fatalf("missing model/base/key code = %q, want %q", code, ProviderFailureNotConfigured)
	}
	if _, code := selectImageComposer(AuthoringProviderConfig{ImageProvider: "openai"}); code != ProviderFailureNotConfigured {
		t.Fatalf("missing model/base/key code = %q, want %q", code, ProviderFailureNotConfigured)
	}
	reg := NewAuthoringProviderRegistry(AuthoringProviderConfig{})
	if reg.TextGenerator != nil || reg.ImageComposer != nil {
		t.Fatalf("registry must not fall back to any default adapter")
	}
	if reg.TextFailure != ProviderFailureNotConfigured || reg.ImageFailure != ProviderFailureNotConfigured {
		t.Fatalf("registry failures = %q/%q, want both provider_not_configured", reg.TextFailure, reg.ImageFailure)
	}
}

// The wire representation of job/branch state never carries provider names,
// models, API URLs, keys, prompts or raw storage keys.
func TestAuthoringWireRepresentationLeaksNothing(t *testing.T) {
	rawKey := "ai-authoring/store/j/c/scene.png"
	job := AuthoringJob{
		ID:                  "job-1",
		StoreID:             "store-1",
		Brief:               "منتج قهوة",
		CreatedBySubject:    "creator-subject",
		IDempotencyKey:      "idem-key",
		RequestedImageCount: 2,
		EffectiveImageLimit: 2,
		Status:              JobStatusPartial,
		TextStatus:          BranchStatusSucceeded,
		ImageStatus:         BranchStatusFailed,
		ErrorCode:           AuthoringProviderNotConfigured,
		ImageCandidates: []AuthoringImageCandidate{{
			ID:                   "c-1",
			StorageKey:           rawKey,
			ForegroundStorageKey: "ai-authoring/store/j/c/foreground.png",
			URL:                  "https://media.example/scene.png",
			ForegroundURL:        "https://media.example/foreground.png",
			ContentType:          "image/png",
			Status:               ItemStatusProposed,
			Visibility:           "hidden",
		}},
	}
	encoded, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	forbidden := []string{"openai", "ai-authoring/", rawKey, "https://api", "storage_key", "api_key", "provider_config"}
	for _, needle := range forbidden {
		if strings.Contains(string(encoded), needle) {
			t.Fatalf("job JSON leaks %q: %s", needle, encoded)
		}
	}
	// The brief is merchant-authored input and stays visible; provider
	// configuration, raw keys and provider identity do not.
	// Derived state cannot claim success when one branch failed.
	if got := DeriveAuthoringJobStatus(BranchStatusSucceeded, BranchStatusFailed); got != JobStatusPartial {
		t.Fatalf("derive(succeeded, failed) = %s, want partial", got)
	}
	if got := DeriveAuthoringJobStatus(BranchStatusFailed, BranchStatusFailed); got != JobStatusFailed {
		t.Fatalf("derive(failed, failed) = %s, want failed", got)
	}
	if got := DeriveAuthoringJobStatus(BranchStatusSucceeded, BranchStatusSucceeded); got != JobStatusSucceeded {
		t.Fatalf("derive(succeeded, succeeded) = %s, want succeeded", got)
	}
}

// generateProductText surface note: the classifier maps unknown errors to
// provider_unavailable and explicit ProviderFailureError keeps its code.
func TestFailureCodeClassification(t *testing.T) {
	if got := failureCode(ProviderFailureError{Code: ProviderFailureRefused}); got != ProviderFailureRefused {
		t.Fatalf("refused code = %q", got)
	}
	if got := failureCode(errGenericTest{}); got != ProviderFailureUnavailable {
		t.Fatalf("unknown error code = %q, want provider_unavailable", got)
	}
}

type errGenericTest struct{}

func (errGenericTest) Error() string { return "unexpected" }
