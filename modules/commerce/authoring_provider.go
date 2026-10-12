package commerce

import "context"

type TextGenerator interface {
	GenerateProductText(ctx context.Context, req TextRequest) (TextResult, error)
}

type ImageComposer interface {
	IsolateForeground(ctx context.Context, req IsolateRequest) (Foreground, error)
	ComposeScene(ctx context.Context, req SceneRequest) (ComposedImage, error)
}

type TextRequest struct {
	Brief        string
	SourceAssets []AuthoringSourceAsset
}

type TextFieldResult struct {
	FieldKey      FieldKey
	Locale        Locale
	Value         string
	AttributeCode string
	CategoryID    *string
}

type TextResult struct {
	Fields []TextFieldResult
}

type IsolateRequest struct {
	SourceAssetID string
	StorageKey    string
	ContentType   string
	// SourceBytes carries the source asset content read in Core so the
	// adapter can derive proven foreground/mask layers from the source's
	// own alpha channel without fabricating them from a provider response.
	SourceBytes []byte
}

type Foreground struct {
	ContentType string
	Bytes       []byte
	MaskBytes   []byte
}

type SceneRequest struct {
	SourceAssetID string
	SceneKey      string
	Foreground    Foreground
}

type ComposedImage struct {
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
