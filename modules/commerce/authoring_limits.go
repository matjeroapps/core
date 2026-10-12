package commerce

import "context"

const MaxAuthoringImages = 7

type ImageGenerationLimitResolver interface {
	EffectiveImageLimit(ctx context.Context, storeID string) (int, error)
}

type ConfigImageLimitResolver struct {
	ConfiguredLimit int
}

func NewConfigImageLimitResolver(configured int) ConfigImageLimitResolver {
	return ConfigImageLimitResolver{ConfiguredLimit: configured}
}

func (r ConfigImageLimitResolver) EffectiveImageLimit(ctx context.Context, storeID string) (int, error) {
	limit := r.ConfiguredLimit
	if limit <= 0 {
		limit = 3
	}
	if limit > MaxAuthoringImages {
		limit = MaxAuthoringImages
	}
	return limit, nil
}

func DefaultAuthoringImageCount(effectiveLimit int) int {
	return ProviderDefaultCount(effectiveLimit)
}
