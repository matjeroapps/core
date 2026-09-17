package commerce

import (
	"time"
)

// StoreMediaAsset is an immutable, store-scoped binary asset.
type StoreMediaAsset struct {
	ID               string    `json:"id"`
	StoreID          string    `json:"store_id"`
	ChecksumSHA256   string    `json:"checksum_sha256"`
	StorageKey       string    `json:"storage_key"`
	ContentType      string    `json:"content_type"`
	ByteSize         int64     `json:"byte_size"`
	OriginalFilename string    `json:"original_filename"`
	Status           string    `json:"status"` // ready, deleting, deleted
	CreatedBySubject string    `json:"created_by_subject"`
	URL              string    `json:"url,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ProductMediaReference is a product/listing specific reference to a StoreMediaAsset.
type ProductMediaReference struct {
	ID        string           `json:"id"`
	StoreID   string           `json:"store_id"`
	ProductID string           `json:"product_id"`
	AssetID   string           `json:"asset_id"`
	AltText   string           `json:"alt_text"`
	SortOrder int              `json:"sort_order"`
	IsPrimary bool             `json:"is_primary"`
	Asset     *StoreMediaAsset `json:"asset,omitempty"`
	URL       string           `json:"url,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// ListStoreMediaAssetsResponse is the paginated response for listing store assets.
type ListStoreMediaAssetsResponse struct {
	Items  []StoreMediaAsset `json:"items"`
	Total  int               `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

// PresignMediaUploadRequest initiates presign or deduplication lookup.
type PresignMediaUploadRequest struct {
	ProductID      string `json:"product_id,omitempty"`
	ClientUploadID string `json:"client_upload_id,omitempty"`
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	SizeBytes      int64  `json:"size_bytes"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

// PresignMediaUploadResponse returns upload mode ("upload" or "reuse").
type PresignMediaUploadResponse struct {
	Mode            string            `json:"mode"`
	Asset           *StoreMediaAsset  `json:"asset,omitempty"`
	IntentID        string            `json:"intent_id,omitempty"`
	UploadURL       string            `json:"upload_url,omitempty"`
	UploadToken     string            `json:"upload_token,omitempty"`
	StorageKey      string            `json:"storage_key,omitempty"`
	RequiredHeaders map[string]string `json:"required_headers,omitempty"`
	ExpiresAt       *time.Time        `json:"expires_at,omitempty"`
}

// AttachMediaReferenceRequest attaches a store asset to a product.
type AttachMediaReferenceRequest struct {
	AssetID   string `json:"asset_id"`
	AltText   string `json:"alt_text"`
	SortOrder int    `json:"sort_order"`
	IsPrimary bool   `json:"is_primary"`
}

// UpdateMediaReferenceRequest updates reference-specific display metadata.
type UpdateMediaReferenceRequest struct {
	AltText   string `json:"alt_text"`
	SortOrder int    `json:"sort_order"`
	IsPrimary bool   `json:"is_primary"`
}
