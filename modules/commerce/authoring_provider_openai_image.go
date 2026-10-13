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
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	// JPEG inputs need decoding for the alpha-capability probe.
	_ "image/jpeg"
)

// openAIImageComposer sends the complete merchant image as a visual reference
// and receives a complete landscape product photograph. Core does not paste a
// cropped foreground over a separately generated background.
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
	if err != nil {
		return Foreground{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	bounds := src.Bounds()
	maskImage, err = resizeProviderImage(maskImage, bounds.Dx(), bounds.Dy(), true)
	if err != nil {
		return Foreground{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
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

// resizeProviderImage normalizes a provider image to requested dimensions.
// For masks, aspect ratio must remain close so resizing cannot shift/crop the
// provider's estimated product silhouette.
func resizeProviderImage(src image.Image, width, height int, preserveAspect bool) (*image.NRGBA, error) {
	if src == nil || width <= 0 || height <= 0 || src.Bounds().Dx() <= 0 || src.Bounds().Dy() <= 0 {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if preserveAspect {
		sourceAspect := float64(sw) / float64(sh)
		targetAspect := float64(width) / float64(height)
		if math.Abs(sourceAspect-targetAspect)/targetAspect > 0.03 {
			return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := (float64(y)+0.5)*float64(sh)/float64(height) - 0.5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		if y0 < 0 {
			y0, fy = 0, 0
		}
		y1 := min(y0+1, sh-1)
		for x := 0; x < width; x++ {
			sx := (float64(x)+0.5)*float64(sw)/float64(width) - 0.5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			if x0 < 0 {
				x0, fx = 0, 0
			}
			x1 := min(x0+1, sw-1)
			c00 := color.NRGBAModel.Convert(src.At(src.Bounds().Min.X+x0, src.Bounds().Min.Y+y0)).(color.NRGBA)
			c10 := color.NRGBAModel.Convert(src.At(src.Bounds().Min.X+x1, src.Bounds().Min.Y+y0)).(color.NRGBA)
			c01 := color.NRGBAModel.Convert(src.At(src.Bounds().Min.X+x0, src.Bounds().Min.Y+y1)).(color.NRGBA)
			c11 := color.NRGBAModel.Convert(src.At(src.Bounds().Min.X+x1, src.Bounds().Min.Y+y1)).(color.NRGBA)
			mix := func(a, b, c, d uint8) uint8 {
				top := float64(a)*(1-fx) + float64(b)*fx
				bottom := float64(c)*(1-fx) + float64(d)*fx
				return uint8(math.Round(top*(1-fy) + bottom*fy))
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: mix(c00.R, c10.R, c01.R, c11.R), G: mix(c00.G, c10.G, c01.G, c11.G),
				B: mix(c00.B, c10.B, c01.B, c11.B), A: mix(c00.A, c10.A, c01.A, c11.A),
			})
		}
	}
	return dst, nil
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

// GenerateProductImage asks the image model to recreate the referenced product in a
// realistic landscape scene. The complete original is retained separately for
// merchant comparison; the output is not a background layer.
func (p *openAIImageComposer) GenerateProductImage(ctx context.Context, req ProductImageRequest) (GeneratedProductImage, error) {
	sourceBytes := req.SourceBytes
	if len(sourceBytes) == 0 {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = http.DetectContentType(sourceBytes)
	}
	var data []byte
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		data, lastErr = p.postProductImageEdit(ctx, sourceBytes, contentType, productImagePrompt(req.SceneKey, req.Variation))
		if lastErr == nil || failureCode(lastErr) != ProviderFailureUnavailable {
			break
		}
	}
	if lastErr != nil {
		return GeneratedProductImage{}, lastErr
	}
	sceneImg, _, err := image.Decode(bytes.NewReader(data))
	if err != nil || sceneImg.Bounds().Dx() <= sceneImg.Bounds().Dy() {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, sceneImg); err != nil {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	return GeneratedProductImage{ContentType: "image/png", Bytes: out.Bytes()}, nil
}

var sceneDirections = []string{
	"A bright, airy home interior with a pale limestone display surface, a large softly lit window, light neutral walls, and subtle sage-green accents.",
	"A warm, tactile interior with a rich walnut display surface, muted terracotta plaster walls, softly blurred shelving, and golden pendant lighting.",
	"A premium contemporary showroom with a deep charcoal stone display surface, dark teal architectural background, restrained copper accents, and dramatic directional lighting.",
	"A crisp Scandinavian retail setting with a white oak display surface, pale blue-gray walls, clean geometric shelving, and cool diffused daylight.",
	"A calm botanical conservatory with a natural rattan display surface, softly blurred foliage, textured warm-white walls, and filtered daylight.",
	"A modern concrete loft with a smooth slate display surface, textured gray architectural walls, strong side light, and long soft shadows.",
	"A refined boutique setting with a light blush display surface, a sculptural sand-colored wall niche, subtle brass details, and soft studio lighting.",
}

func productImagePrompt(sceneKey string, variation int) string {
	index := variation - 1
	if index < 0 {
		index = 0
	}
	direction := sceneDirections[index%len(sceneDirections)]
	return "Use the uploaded product photo as the visual reference. Identify the actual product and create a new, complete, photorealistic commercial product photograph showing that product naturally placed in a realistic environment suitable for: " + sceneKey + ". " + direction + " Compose a wide landscape image (3:2, 1536x1024) with the entire product visible, comfortably framed with clear space around it, natural perspective, believable contact shadows, and lighting that integrates the product into the scene. Preserve the recognizable product design, silhouette, proportions, primary colors, materials, and visible brand identity as closely as possible. Do not crop the product, turn it into a cutout, paste the source photo over a new background, or merely replace the background. The source is a reference, not a pixel layer; recreate the product as needed for a coherent realistic photograph. Do not add text, labels, accessories, or product features that are not supported by the reference."
}

func (p *openAIImageComposer) postProductImageEdit(ctx context.Context, source []byte, contentType, prompt string) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := [][2]string{
		{"model", p.model},
		{"prompt", prompt},
		{"size", "1536x1024"},
		{"output_format", "png"},
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
		}
	}
	extension := ".png"
	switch contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/webp":
		extension = ".webp"
	}
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", `form-data; name="image"; filename="product`+extension+`"`)
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if _, err := part.Write(source); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	if err := writer.Close(); err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/images/edits", &body)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := p.client.Do(request)
	if err != nil {
		return nil, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	defer func() { _ = response.Body.Close() }()
	return p.decodeImageResponse(response)
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
