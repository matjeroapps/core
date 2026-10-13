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
//   - transparent source images use their own alpha locally. For opaque source
//     images, OpenAI's transparent PNG edit is used only for its approximate
//     alpha mask; foreground RGB always comes from the merchant's original.
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

const isolateForegroundPrompt = "Create a transparent PNG cutout of the product in this image. Preserve the product silhouette and include the complete product, excluding the background. This output is used only as an approximate alpha mask; do not add shadows or other objects."

func (p *openAIImageComposer) IsolateForeground(ctx context.Context, req IsolateRequest) (Foreground, error) {
	// Preferred path: isolation is derivable in Core from the source itself.
	if fg, ok := foregroundFromSourceAlpha(req.SourceBytes); ok {
		return fg, nil
	}
	// The provider result supplies only a best-effort alpha mask. Its colors
	// are discarded so the generated edit can never recolor or replace the
	// merchant's product pixels.
	maskImage, err := p.postImagesEdits(ctx, req)
	if err != nil {
		return Foreground{}, err
	}
	return foregroundWithProviderAlpha(req.SourceBytes, maskImage)
}

func foregroundWithProviderAlpha(source []byte, maskImage image.Image) (Foreground, error) {
	if len(source) == 0 || maskImage == nil {
		return Foreground{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	src, _, err := image.Decode(bytes.NewReader(source))
	if err != nil || src.Bounds().Size() != maskImage.Bounds().Size() {
		return Foreground{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	bounds := src.Bounds()
	maskBounds := maskImage.Bounds()
	foreground := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	mask := image.NewAlpha(foreground.Bounds())
	hasForeground, hasBackground := false, false
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			_, _, _, alpha16 := maskImage.At(maskBounds.Min.X+x, maskBounds.Min.Y+y).RGBA()
			alpha := uint8(alpha16 >> 8)
			if alpha == 0 {
				hasBackground = true
			} else {
				hasForeground = true
			}
			original := color.NRGBAModel.Convert(src.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			original.A = alpha
			foreground.SetNRGBA(x, y, original)
			mask.SetAlpha(x, y, color.Alpha{A: alpha})
		}
	}
	if !hasForeground || !hasBackground {
		return Foreground{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	var foregroundBytes, maskBytes bytes.Buffer
	if err := png.Encode(&foregroundBytes, foreground); err != nil {
		return Foreground{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if err := png.Encode(&maskBytes, mask); err != nil {
		return Foreground{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	return Foreground{ContentType: "image/png", Bytes: foregroundBytes.Bytes(), MaskBytes: maskBytes.Bytes()}, nil
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
			alpha := uint8(a >> 8)
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

// postImagesEdits requests a transparent PNG and returns its decoded alpha
// source. The returned RGB is never used as product content.
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
	if err := writer.WriteField("background", "transparent"); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if err := writer.WriteField("output_format", "png"); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = http.DetectContentType(req.SourceBytes)
	}
	extension := ".png"
	switch contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/webp":
		extension = ".webp"
	}
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", `form-data; name="image"; filename="source`+extension+`"`)
	partHeader.Set("Content-Type", contentType)
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
	layer, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if format != "png" {
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
