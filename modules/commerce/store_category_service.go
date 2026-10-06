package commerce

import (
	"context"
	"fmt"
	"regexp"
)

// Service methods for store-scoped categories. Every method authorizes the
// subject against the target store's seller membership before touching data,
// and every foreign/unknown id resolves to ErrNotFound so callers cannot
// distinguish "another seller's category" from "no such category".

var storeCategorySlugRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Supported content locales for store categories. The English name is
// mandatory; Arabic is optional with display fallback to English.
const (
	storeCategoryLocaleEN = "en"
	storeCategoryLocaleAR = "ar"
)

func validateStoreCategorySlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("%w: slug is required", ErrInvalidInput)
	}
	if len(slug) > 200 {
		return fmt.Errorf("%w: slug must be at most 200 characters", ErrInvalidInput)
	}
	if !storeCategorySlugRegex.MatchString(slug) {
		return fmt.Errorf("%w: slug must be lowercase letters, digits and hyphens with no leading or trailing hyphen", ErrInvalidInput)
	}
	return nil
}

func validateStoreCategoryTranslations(translations []StoreCategoryTranslation) error {
	byLocale := make(map[string]StoreCategoryTranslation, len(translations))
	for _, tr := range translations {
		if tr.Locale != storeCategoryLocaleEN && tr.Locale != storeCategoryLocaleAR {
			return fmt.Errorf("%w: unsupported locale %q (supported: en, ar)", ErrInvalidInput, tr.Locale)
		}
		if _, seen := byLocale[tr.Locale]; seen {
			return fmt.Errorf("%w: duplicate locale %q", ErrInvalidInput, tr.Locale)
		}
		if tr.Name == "" {
			return fmt.Errorf("%w: name is required for locale %q", ErrInvalidInput, tr.Locale)
		}
		byLocale[tr.Locale] = tr
	}
	if _, ok := byLocale[storeCategoryLocaleEN]; !ok {
		return fmt.Errorf("%w: an English (en) name is required", ErrInvalidInput)
	}
	return nil
}

func (s Service) CreateStoreCategoryForSubject(ctx context.Context, subject, storeID, slug string, parentCategoryID *string, sortOrder *int, translations []StoreCategoryTranslation) (StoreCategoryWithMeta, error) {
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	if sortOrder == nil {
		sortOrder = new(int)
	}
	if err := validateStoreCategorySlug(slug); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	if err := validateStoreCategoryTranslations(translations); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	if parentCategoryID != nil {
		if _, err := s.getStoreCategoryForStore(ctx, storeID, *parentCategoryID); err != nil {
			return StoreCategoryWithMeta{}, err
		}
	}

	return s.repo.CreateStoreCategory(ctx, storeID, slug, parentCategoryID, *sortOrder, translations)
}

func (s Service) ListStoreCategoriesForSubject(ctx context.Context, subject, storeID, statusFilter string, limit, offset int) ([]StoreCategoryWithMeta, int, error) {
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return nil, 0, err
	}
	return s.repo.ListStoreCategories(ctx, storeID, statusFilter, limit, offset)
}

func (s Service) GetStoreCategoryForSubject(ctx context.Context, subject, storeID, categoryID string) (StoreCategoryWithMeta, error) {
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	return s.getStoreCategoryForStore(ctx, storeID, categoryID)
}

func (s Service) UpdateStoreCategoryForSubject(ctx context.Context, subject, storeID, categoryID string, patch StoreCategoryPatch) (StoreCategoryWithMeta, error) {
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	existing, err := s.getStoreCategoryForStore(ctx, storeID, categoryID)
	if err != nil {
		return StoreCategoryWithMeta{}, err
	}
	if patch.Slug != nil {
		if err := validateStoreCategorySlug(*patch.Slug); err != nil {
			return StoreCategoryWithMeta{}, err
		}
	}
	if patch.Translations != nil {
		if err := validateStoreCategoryTranslations(patch.Translations); err != nil {
			return StoreCategoryWithMeta{}, err
		}
	}

	newParent := existing.ParentCategoryID
	if patch.ClearParent {
		newParent = nil
	} else if patch.ParentCategoryID != nil {
		parentID := *patch.ParentCategoryID
		if parentID == categoryID {
			return StoreCategoryWithMeta{}, fmt.Errorf("%w: a category cannot be its own parent", ErrInvalidInput)
		}
		if _, err := s.getStoreCategoryForStore(ctx, storeID, parentID); err != nil {
			return StoreCategoryWithMeta{}, err
		}
		newParent = &parentID
	}
	// Circular-hierarchy guard: reparenting under a descendant (directly or
	// transitively) must be rejected before any write happens.
	if newParent != nil && (patch.ClearParent || patch.ParentCategoryID != nil) {
		ancestors, err := s.repo.GetStoreCategoryAncestorIDs(ctx, storeID, *newParent)
		if err != nil {
			return StoreCategoryWithMeta{}, err
		}
		for _, id := range ancestors {
			if id == categoryID {
				return StoreCategoryWithMeta{}, fmt.Errorf("%w: cannot move a category under one of its own descendants", ErrInvalidInput)
			}
		}
	}

	if patch.Slug == nil && !patch.ClearParent && patch.ParentCategoryID == nil && patch.SortOrder == nil && patch.Translations == nil {
		return existing, nil
	}
	if _, err := s.repo.UpdateStoreCategory(ctx, storeID, categoryID, patch); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	return s.getStoreCategoryForStore(ctx, storeID, categoryID)
}

func (s Service) TransitionStoreCategoryForSubject(ctx context.Context, subject, storeID, categoryID, status string) (StoreCategoryWithMeta, error) {
	if !IsValidStoreCategoryStatus(status) {
		return StoreCategoryWithMeta{}, fmt.Errorf("%w: unknown status %q", ErrInvalidInput, status)
	}
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	if _, err := s.getStoreCategoryForStore(ctx, storeID, categoryID); err != nil {
		return StoreCategoryWithMeta{}, err
	}
	return s.repo.UpdateStoreCategoryStatus(ctx, storeID, categoryID, status)
}

func (s Service) DeleteStoreCategoryForSubject(ctx context.Context, subject, storeID, categoryID string) error {
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return err
	}
	if _, err := s.getStoreCategoryForStore(ctx, storeID, categoryID); err != nil {
		return err
	}
	// Deletion is only for empty leaves (see FR-007a): the repository guard
	// rejects categories with assignments or children with an actionable
	// "archive instead" error.
	return s.repo.DeleteStoreCategory(ctx, storeID, categoryID)
}

func (s Service) ReorderStoreCategoriesForSubject(ctx context.Context, subject, storeID string, order []StoreCategoryOrder) error {
	if len(order) == 0 {
		return fmt.Errorf("%w: reorder payload must contain at least one entry", ErrInvalidInput)
	}
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return err
	}
	return s.repo.ReorderStoreCategories(ctx, storeID, order)
}

// SetStoreProductCategoriesForSubject replaces a product's store-category
// assignments. Every submitted category must belong to the same store as the
// product's listing; cross-store or unknown ids are rejected with a validation
// error before any write.
func (s Service) SetStoreProductCategoriesForSubject(ctx context.Context, subject, storeID, productID string, categoryIDs []string) error {
	if _, err := s.authorizeStoreCategoryAccess(ctx, subject, storeID); err != nil {
		return err
	}
	if _, err := s.repo.GetSellerListingByStoreAndProduct(ctx, storeID, productID); err != nil {
		return err
	}
	if err := s.validateStoreCategoryAssignment(ctx, storeID, categoryIDs); err != nil {
		return err
	}
	return s.repo.SetStoreProductCategories(ctx, productID, storeID, categoryIDs)
}

// validateStoreCategoryAssignment enforces FR-013: a product can only be
// assigned to categories of its own store.
func (s Service) validateStoreCategoryAssignment(ctx context.Context, storeID string, categoryIDs []string) error {
	if len(categoryIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(categoryIDs))
	for _, id := range categoryIDs {
		if id == "" {
			return fmt.Errorf("%w: category id cannot be empty", ErrInvalidInput)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("%w: duplicate category id %q", ErrInvalidInput, id)
		}
		seen[id] = struct{}{}
	}
	ok, err := s.repo.StoreCategoriesBelongToStore(ctx, storeID, categoryIDs)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: categories must belong to the product's store", ErrInvalidInput)
	}
	return nil
}

func (s Service) authorizeStoreCategoryAccess(ctx context.Context, subject, storeID string) (Seller, error) {
	if storeID == "" {
		return Seller{}, ErrInvalidInput
	}
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return Seller{}, err
	}
	return s.RequireSellerManagerAccess(ctx, subject, store.SellerID)
}

func (s Service) getStoreCategoryForStore(ctx context.Context, storeID, categoryID string) (StoreCategoryWithMeta, error) {
	// Scoped by (store_id, id): a foreign category is indistinguishable from a
	// missing one — both return ErrNotFound.
	return s.repo.GetStoreCategory(ctx, storeID, categoryID)
}
