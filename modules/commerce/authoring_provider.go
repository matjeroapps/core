package commerce

import "context"

type TextGenerator interface {
	GenerateProductText(ctx context.Context, req TextRequest) (TextResult, error)
}

type ImageComposer interface {
	IsolateForeground(ctx context.Context, req IsolateRequest) (Foreground, error)
	GenerateProductImage(ctx context.Context, req ProductImageRequest) (GeneratedProductImage, error)
}

type TextRequest struct {
	Brief        string
	SourceAssets []AuthoringSourceAsset
}

type TextFieldResult struct {
	FieldKey      FieldKey `json:"field_key"`
	Locale        Locale   `json:"locale"`
	Value         string   `json:"value"`
	AttributeCode string   `json:"attribute_code,omitempty"`
	CategoryID    *string  `json:"category_id,omitempty"`
}

type TextResult struct {
	Fields []TextFieldResult
}

type IsolateRequest struct {
	SourceAssetID string
	StorageKey    string
	ContentType   string
	// SourceBytes carries source content for optional foreground isolation.
	// Product image generation uses ProductImageRequest.
	SourceBytes []byte
}

type Foreground struct {
	ContentType string
	Bytes       []byte
	MaskBytes   []byte
}

type ProductImageRequest struct {
	SourceAssetID string
	ContentType   string
	SourceBytes   []byte
	SceneKey      string
	Variation     int
}

type GeneratedProductImage struct {
	ContentType string
	Bytes       []byte
}

type ProviderFailureError struct {
	Code string
}

func (e ProviderFailureError) Error() string {
	if e.Code == "" {
		return ProviderFailureUnavailable
	}
	return e.Code
}
