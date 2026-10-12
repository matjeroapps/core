package commerce

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	// JPEG inputs need decoding for the alpha-capability probe.
	_ "image/jpeg"
)

// openAIImageComposer implements the image branch against OpenAI's real
// image endpoints:
//
//   - foreground/mask isolation is never fabricated from a raw provider
//     response. OpenAI's /images/edits returns a single edited image and no
//     separately-named foreground/mask pair, so when the Core input (the
//     source asset) carries its own alpha channel the adapter derives the
//     product mask from that alpha locally; when it does not, no provider
//     call can produce a provable isolation and the adapter fails
//     classified (provider_capability_unsupported) instead of pretending.
//   - scene generation posts the documented /images/generations JSON body
//     and decodes data[0].b64_json or downloads data[0].url for real; the
//     scene response is never the request foreground echoed back.
type openAIImageComposer struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAIImageComposer(baseURL, apiKey, model string, timeout time.Duration) ImageComposer {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &openAIImageComposer{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, client: &http.Client{Timeout: timeout}}
}

const isolateForegroundPrompt = "Isolate the product foreground only: keep the product pixels unchanged and make every pixel outside the product fully transparent. Do not redraw the product."

func (p *openAIImageComposer) IsolateForeground(ctx context.Context, req IsolateRequest) (Foreground, error) {
	// Preferred path: isolation is derivable in Core from the source itself.
	if fg, ok := foregroundFromSourceAlpha(req.SourceBytes); ok {
		return fg, nil
	}
	// Fallback path: ask the images-edits endpoint for an isolated product
	// layer. Even when that call succeeds, the response is a single image
	// without a separately-declared mask, so without source alpha no
	// provable isolation exists: fail classified instead of treating the
	// raw response image as its own mask.
	if _, err := p.postImagesEdits(ctx, req); err != nil {
		return Foreground{}, err
	}
	return Foreground{}, ProviderFailureError{Code: ProviderFailureCapabilityUnsupported}
}

// foregroundFromSourceAlpha decodes the source bytes and, when they contain
// real transparency, derives the product mask (source alpha) and returns the
// foreground as a re-encoded PNG. When the source has no alpha channel the
// result is not ok and nothing is fabricated.
func foregroundFromSourceAlpha(source []byte) (Foreground, bool) {
	if len(source) == 0 {
		return Foreground{}, false
	}
	src, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return Foreground{}, false
	}
	bounds := src.Bounds()
	switch src.ColorModel() {
	case color.RGBAModel, color.NRGBAModel, color.AlphaModel, color.Gray16Model:
	default:
		// Opaque models (e.g. JPEG-style) carry no alpha channel at all.
		return Foreground{}, false
	}
	hasAlpha := false
	for y := bounds.Min.Y; y < bounds.Max.Y && !hasAlpha; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()
			if a != 0xffff {
				hasAlpha = true
			}
		}
	}
	if !hasAlpha {
		return Foreground{}, false
	}
	fgRGBA := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(fgRGBA, fgRGBA.Bounds(), src, bounds.Min, draw.Src)
	mask := image.NewAlpha(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			_, _, _, a := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			alpha := uint8(0)
			if a >= 0x8000 {
				alpha = 0xff
			}
			mask.SetAlpha(x, y, color.Alpha{A: alpha})
		}
	}
	var fgBuf, maskBuf bytes.Buffer
	if err := png.Encode(&fgBuf, fgRGBA); err != nil {
		return Foreground{}, false
	}
	if err := png.Encode(&maskBuf, mask); err != nil {
		return Foreground{}, false
	}
	return Foreground{ContentType: "image/png", Bytes: fgBuf.Bytes(), MaskBytes: maskBuf.Bytes()}, true
}

// postImagesEdits sends the real multipart form documented for
// POST {baseURL}/images/edits and validates a single-image response. The
// parsed layer is deliberately discarded: an edit response is never reused
// as a mask or as a proven foreground.
func (p *openAIImageComposer) postImagesEdits(ctx context.Context, req IsolateRequest) (image.Image, error) {
	if len(req.SourceBytes) == 0 {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		layer, err := p.postImagesEditsOnce(ctx, req)
		if err == nil {
			return layer, nil
		}
		lastErr = err
		if failureCode(err) != ProviderFailureUnavailable {
			break
		}
	}
	return nil, lastErr
}

func (p *openAIImageComposer) postImagesEditsOnce(ctx context.Context, req IsolateRequest) (image.Image, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", p.model); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if err := writer.WriteField("prompt", isolateForegroundPrompt); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", `form-data; name="image"; filename="source.png"`)
	partHeader.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if _, err := part.Write(req.SourceBytes); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if err := writer.Close(); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/images/edits", &body)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := p.decodeImageResponse(resp)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	layer, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	return layer, nil
}

// ComposeScene posts the documented JSON body for POST
// {baseURL}/images/generations and decodes data[0].b64_json or downloads
// data[0].url; the returned bytes are always the fetched scene.
func (p *openAIImageComposer) ComposeScene(ctx context.Context, req SceneRequest) (ComposedImage, error) {
	if len(req.Foreground.Bytes) == 0 || len(req.Foreground.MaskBytes) == 0 {
		return ComposedImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	payload := map[string]any{
		"model":  p.model,
		"prompt": "Generate the surrounding scene surface only: an empty studio background for scene key " + req.SceneKey + ". Leave an unrendered central product area; do not include or redraw any product.",
		"n":      1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ComposedImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	data, err := p.doImageJSON(ctx, p.baseURL+"/images/generations", body)
	if err != nil {
		return ComposedImage{}, err
	}
	if len(data) == 0 {
		return ComposedImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	sceneImg, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ComposedImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, sceneImg); err != nil {
		return ComposedImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	return ComposedImage{ContentType: "image/png", Bytes: out.Bytes()}, nil
}

// doImageJSON performs one JSON POST against an OpenAI images endpoint with
// a single internal retry for transport/timeouts only.
func (p *openAIImageComposer) doImageJSON(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		data, err := p.doImageJSONOnce(ctx, endpoint, body)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if failureCode(err) != ProviderFailureUnavailable {
			break
		}
	}
	return nil, lastErr
}

func (p *openAIImageComposer) doImageJSONOnce(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	defer func() { _ = resp.Body.Close() }()
	return p.decodeImageResponse(resp)
}

// decodeImageResponse classifies HTTP-level failures and parses the
// documented images response envelope.
func (p *openAIImageComposer) decodeImageResponse(resp *http.Response) ([]byte, error) {
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ProviderFailureError{Code: ProviderFailureRefused}
	}
	var decoded struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if len(decoded.Data) == 0 {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	item := decoded.Data[0]
	if item.B64JSON != "" {
		raw, err := base64.StdEncoding.DecodeString(item.B64JSON)
		if err != nil || len(raw) == 0 {
			return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
		}
		return raw, nil
	}
	if strings.TrimSpace(item.URL) == "" {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	dlReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, item.URL, nil)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	dlResp, err := p.client.Do(dlReq)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	defer func() { _ = dlResp.Body.Close() }()
	if dlResp.StatusCode < 200 || dlResp.StatusCode >= 300 {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	raw, err := io.ReadAll(io.LimitReader(dlResp.Body, 16*1024*1024))
	if err != nil || len(raw) == 0 {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	return raw, nil
}
