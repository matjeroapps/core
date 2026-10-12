package commerce

import "time"

type FieldKey string

const (
	FieldKeyName        FieldKey = "name"
	FieldKeyDescription FieldKey = "description"
	FieldKeyKeywords    FieldKey = "keywords"
	FieldKeyCategory    FieldKey = "category"
	FieldKeyAttribute   FieldKey = "attribute"
)

type Locale string

const (
	LocaleAR Locale = "ar"
	LocaleEN Locale = "en"
)

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusPartial    JobStatus = "partial"
	JobStatusSucceeded  JobStatus = "succeeded"
	JobStatusFailed     JobStatus = "failed"
)

type BranchStatus string

const (
	BranchStatusPending    BranchStatus = "pending"
	BranchStatusProcessing BranchStatus = "processing"
	BranchStatusSucceeded  BranchStatus = "succeeded"
	BranchStatusFailed     BranchStatus = "failed"
)

type ItemStatus string

const (
	ItemStatusProposed ItemStatus = "proposed"
	ItemStatusAccepted ItemStatus = "accepted"
	ItemStatusRejected ItemStatus = "rejected"
	ItemStatusFailed   ItemStatus = "failed"
)

const (
	ProviderFailureNotConfigured         = "provider_not_configured"
	ProviderFailureUnsupported           = "provider_unsupported"
	ProviderFailureCapabilityUnsupported = "provider_capability_unsupported"
	ProviderFailureUnavailable           = "provider_unavailable"
	ProviderFailureRefused               = "provider_refused"
	ProviderFailureInvalidOutput         = "provider_invalid_output"
	AuthoringProviderNotConfigured       = "ai_provider_not_configured"
	AuthoringUnsupportedField            = "unsupported_field"
)

func IsValidFieldKey(key FieldKey) bool {
	switch key {
	case FieldKeyName, FieldKeyDescription, FieldKeyKeywords, FieldKeyCategory, FieldKeyAttribute:
		return true
	default:
		return false
	}
}

func IsValidLocale(locale Locale) bool { return locale == LocaleAR || locale == LocaleEN }

func DeriveAuthoringJobStatus(textStatus, imageStatus BranchStatus) JobStatus {
	if textStatus == BranchStatusSucceeded && imageStatus == BranchStatusSucceeded {
		return JobStatusSucceeded
	}
	if (textStatus == BranchStatusSucceeded && imageStatus == BranchStatusFailed) || (textStatus == BranchStatusFailed && imageStatus == BranchStatusSucceeded) {
		return JobStatusPartial
	}
	if textStatus == BranchStatusFailed && imageStatus == BranchStatusFailed {
		return JobStatusFailed
	}
	if textStatus == BranchStatusProcessing || imageStatus == BranchStatusProcessing {
		return JobStatusProcessing
	}
	return JobStatusPending
}

func ProviderDefaultCount(effectiveLimit int) int {
	if effectiveLimit <= 0 {
		return 1
	}
	if effectiveLimit < 3 {
		return effectiveLimit
	}
	return 3
}

type AuthoringJob struct {
	ID                  string                    `json:"id"`
	StoreID             string                    `json:"store_id"`
	CreatedBySubject    string                    `json:"-"`
	Brief               string                    `json:"brief"`
	RequestedImageCount int                       `json:"requested_image_count"`
	EffectiveImageLimit int                       `json:"effective_image_limit"`
	Status              JobStatus                 `json:"status"`
	TextStatus          BranchStatus              `json:"text_status"`
	ImageStatus         BranchStatus              `json:"image_status"`
	IDempotencyKey      string                    `json:"-"`
	Attempts            int                       `json:"attempts"`
	ErrorCode           string                    `json:"error_code"`
	ErrorMessage        string                    `json:"error_message"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
	StartedAt           *time.Time                `json:"started_at"`
	CompletedAt         *time.Time                `json:"completed_at"`
	Fields              []AuthoringFieldProposal  `json:"fields"`
	ImageCandidates     []AuthoringImageCandidate `json:"image_candidates"`
}

type AuthoringSourceAsset struct {
	JobID              string
	AssetID            string
	StoreID            string
	SourceOrder        int
	IsPrimaryReference bool
}

type AuthoringFieldProposal struct {
	ID              string     `json:"id"`
	JobID           string     `json:"-"`
	StoreID         string     `json:"-"`
	FieldKey        FieldKey   `json:"field_key"`
	Locale          Locale     `json:"locale"`
	Value           string     `json:"value"`
	AttributeCode   string     `json:"attribute_code"`
	CategoryID      *string    `json:"category_id"`
	Status          ItemStatus `json:"status"`
	EditedBySubject string     `json:"edited_by_subject"`
	FailureCode     string     `json:"failure_code"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type AuthoringImageCandidate struct {
	ID                   string     `json:"id"`
	JobID                string     `json:"job_id,omitempty"`
	StoreID              string     `json:"store_id"`
	SourceAssetID        string     `json:"source_asset_id"`
	SceneKey             string     `json:"scene_key"`
	StorageKey           string     `json:"-"`
	ForegroundStorageKey string     `json:"-"`
	URL                  string     `json:"url,omitempty"`
	ForegroundURL        string     `json:"foreground_url,omitempty"`
	ContentType          string     `json:"content_type"`
	AltText              string     `json:"alt_text"`
	ByteSize             int64      `json:"byte_size"`
	Visibility           string     `json:"visibility"`
	Status               ItemStatus `json:"status"`
	AssetID              *string    `json:"asset_id"`
	ProductID            *string    `json:"product_id,omitempty"`
	FailureCode          string     `json:"failure_code"`
	RetryCount           int        `json:"retry_count"`
	UpdatedAt            time.Time  `json:"updated_at"`
}
