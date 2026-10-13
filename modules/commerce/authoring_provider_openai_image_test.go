package commerce

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAlphaForegroundPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if y >= 2 {
				alpha := uint8(255)
				if x == 0 && y == 2 {
					alpha = 128
				}
				img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 40, B: 40, A: alpha})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func testOpaqueForegroundJPG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func testSceneResponsePNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 60, B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// TestAdapterIsolateForegroundFromSourceAlpha proves the adapter derives the
// mask from the source's own alpha and never needs a fabricated provider mask.
func TestAdapterIsolateForegroundFromSourceAlpha(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeImageB64Response(t, w, testSceneResponsePNG())
	}))
	defer server.Close()
	adapter := NewOpenAIImageComposer(server.URL, "secret-token", "gpt-image-1", 0)
	fg, err := adapter.IsolateForeground(context.Background(), IsolateRequest{StorageKey: "k", ContentType: "image/png", SourceBytes: testAlphaForegroundPNG()})
	if err != nil {
		t.Fatalf("IsolateForeground with alpha source: %v", err)
	}
	if calls != 0 {
		t.Fatalf("provider must not be called when source alpha proves isolation, got %d calls", calls)
	}
	fgImg, _, err := image.Decode(bytes.NewReader(fg.Bytes))
	if err != nil {
		t.Fatalf("decode foreground: %v", err)
	}
	if _, _, _, alpha := fgImg.At(0, 0).RGBA(); alpha != 0 {
		t.Fatalf("foreground background pixel must stay transparent, got alpha %d", alpha)
	}
	if _, _, _, alpha := fgImg.At(0, 3).RGBA(); alpha != 0xffff {
		t.Fatalf("foreground product pixel must stay opaque, got alpha %d", alpha)
	}
	maskImg, _, err := image.Decode(bytes.NewReader(fg.MaskBytes))
	if err != nil {
		t.Fatalf("decode mask: %v", err)
	}
	maskNorm := image.NewAlpha(maskImg.Bounds())
	draw.Draw(maskNorm, maskNorm.Bounds(), maskImg, maskImg.Bounds().Min, draw.Src)
	if maskNorm.AlphaAt(0, 3).A != 0xff || maskNorm.AlphaAt(0, 0).A != 0 {
		t.Fatalf("mask must mark the product region only, got product=%d background=%d", maskNorm.AlphaAt(0, 3).A, maskNorm.AlphaAt(0, 0).A)
	}
	if got := maskNorm.AlphaAt(0, 2).A; got != 128 {
		t.Fatalf("source soft edge alpha = %d, want 128", got)
	}
	if _, _, _, got := fgImg.At(0, 2).RGBA(); got != 128*257 {
		t.Fatalf("source soft edge foreground alpha = %d, want %d", got, 128*257)
	}
}

// TestAdapterIsolateForegroundUsesOnlyProviderAlpha proves that opaque source
// colors remain authoritative while OpenAI supplies only an approximate mask.
func TestAdapterIsolateForegroundUsesOnlyProviderAlpha(t *testing.T) {
	mask := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			mask.SetNRGBA(x, y, color.NRGBA{R: 255, B: 255, A: 255})
		}
	}
	var maskBytes bytes.Buffer
	if err := png.Encode(&maskBytes, mask); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Fatalf("edits path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth == "" || !strings.HasPrefix(auth, "Bearer secret-token") {
			t.Fatalf("missing bearer header")
		}
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			t.Fatalf("multipart expected: %v", err)
		}
		if r.PostFormValue("model") != "gpt-image-1" {
			t.Fatalf("model field = %q", r.PostFormValue("model"))
		}
		if r.PostFormValue("background") != "transparent" || r.PostFormValue("output_format") != "png" {
			t.Fatalf("transparent PNG request fields missing")
		}
		file, header, err := r.FormFile("image")
		if err != nil || header == nil {
			t.Fatalf("image form file missing: %v", err)
		}
		if header.Filename != "source.jpg" || header.Header.Get("Content-Type") != "image/jpeg" {
			t.Fatalf("source file metadata = %q / %q", header.Filename, header.Header.Get("Content-Type"))
		}
		defer file.Close()
		writeImageB64Response(t, w, maskBytes.Bytes())
	}))
	defer server.Close()
	adapter := NewOpenAIImageComposer(server.URL, "secret-token", "gpt-image-1", 0)
	fg, err := adapter.IsolateForeground(context.Background(), IsolateRequest{StorageKey: "k", ContentType: "image/jpeg", SourceBytes: testOpaqueForegroundJPG()})
	if err != nil {
		t.Fatalf("opaque source isolation: %v", err)
	}
	fgImage, _, err := image.Decode(bytes.NewReader(fg.Bytes))
	if err != nil {
		t.Fatalf("decode original-color foreground: %v", err)
	}
	if _, _, _, alpha := fgImage.At(0, 0).RGBA(); alpha != 0 {
		t.Fatalf("provider transparent region alpha = %d, want 0", alpha)
	}
	r, g, b, alpha := fgImage.At(0, 3).RGBA()
	if !(g > r && g > b && alpha == 0xffff) {
		t.Fatalf("foreground color = %#04x/%#04x/%#04x alpha %#04x, want original green JPEG color", r, g, b, alpha)
	}
}

func TestAdapterIsolateForegroundRejectsInvalidProviderMasks(t *testing.T) {
	makeMask := func(t *testing.T, bounds image.Rectangle, alpha uint8) []byte {
		t.Helper()
		img := image.NewNRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, A: alpha})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	cases := []struct {
		name string
		mask []byte
	}{
		{name: "different dimensions", mask: makeMask(t, image.Rect(0, 0, 3, 4), 255)},
		{name: "fully opaque", mask: makeMask(t, image.Rect(0, 0, 4, 4), 255)},
		{name: "fully transparent", mask: makeMask(t, image.Rect(0, 0, 4, 4), 0)},
		{name: "undecodable image", mask: []byte("not an image")},
		{name: "non PNG response", mask: testOpaqueForegroundJPG()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeImageB64Response(t, w, tc.mask)
			}))
			defer server.Close()
			adapter := NewOpenAIImageComposer(server.URL, "secret-token", "gpt-image-1", 0)
			_, err := adapter.IsolateForeground(context.Background(), IsolateRequest{ContentType: "image/jpeg", SourceBytes: testOpaqueForegroundJPG()})
			if got := failureCode(err); got != ProviderFailureInvalidOutput {
				t.Fatalf("failure code = %q, want %q (err=%v)", got, ProviderFailureInvalidOutput, err)
			}
		})
	}
}

// TestAdapterComposeSceneRequestsAndParses proves the real request body,
// the bearer header, and the b64/url parsing: the returned bytes are the
// fetched scene, never the foreground echo.
func TestAdapterComposeSceneRequestsAndParses(t *testing.T) {
	scenePNG := testSceneResponsePNG()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/images/generations":
			if auth := r.Header.Get("Authorization"); auth != "Bearer secret-token" {
				t.Fatalf("authorization header = %q", auth)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content type = %q", ct)
			}
			if !strings.Contains(readRequestBodyString(t, r), `"model":"gpt-image-1"`) {
				t.Fatalf("request body missing model")
			}
			writeImageB64Response(t, w, scenePNG)
		case "/scene-file":
			if _, err := w.Write(scenePNG); err != nil {
				t.Fatalf("write file: %v", err)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter := NewOpenAIImageComposer(server.URL, "secret-token", "gpt-image-1", 0)
	fg := validAlphaForegroundForCompose(t)

	scene, err := adapter.ComposeScene(context.Background(), SceneRequest{SceneKey: "coffee-stand", Foreground: fg})
	if err != nil {
		t.Fatalf("ComposeScene b64: %v", err)
	}
	if scene.ContentType != "image/png" || len(scene.Bytes) == 0 {
		t.Fatalf("scene = %s/%d bytes", scene.ContentType, len(scene.Bytes))
	}
	if string(scene.Bytes) == string(fg.Bytes) {
		t.Fatalf("scene must not echo the foreground")
	}

	// URL variant.
	urlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/images/generations" {
			writeImageURLResponse(t, w, server.URL+"/scene-file")
			return
		}
		if r.URL.Path != "/scene-file" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if _, err := w.Write(scenePNG); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}))
	defer urlServer.Close()
	urlAdapter := NewOpenAIImageComposer(urlServer.URL, "secret-token", "gpt-image-1", 0)
	scene, err = urlAdapter.ComposeScene(context.Background(), SceneRequest{SceneKey: "coffee-stand", Foreground: fg})
	if err != nil {
		t.Fatalf("ComposeScene url: %v", err)
	}
	if len(scene.Bytes) != len(scenePNG) {
		t.Fatalf("url scene bytes = %d, want %d", len(scene.Bytes), len(scenePNG))
	}
}

func TestAdapterComposeSceneProviderErrors(t *testing.T) {
	fg := validAlphaForegroundForCompose(t)
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusInternalServerError, "boom", ProviderFailureUnavailable},
		{http.StatusTooManyRequests, "rate", ProviderFailureUnavailable},
		{http.StatusBadRequest, `{"error":"bad request"}`, ProviderFailureRefused},
		{http.StatusOK, `{"data":[]}`, ProviderFailureInvalidOutput},
		{http.StatusOK, `{"data":[{"b64_json":"not-base64!!"}]}`, ProviderFailureInvalidOutput},
		{http.StatusOK, `{"data":[{"b64_json":""}]}`, ProviderFailureInvalidOutput},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			if _, err := w.Write([]byte(tc.body)); err != nil {
				t.Fatalf("write body: %v", err)
			}
		}))
		adapter := NewOpenAIImageComposer(server.URL, "secret-token", "gpt-image-1", 0)
		_, err := adapter.ComposeScene(context.Background(), SceneRequest{SceneKey: "k", Foreground: fg})
		server.Close()
		if err == nil {
			t.Fatalf("status %d body %s must fail", tc.status, tc.body)
		}
		if got := failureCode(err); got != tc.want {
			t.Fatalf("status %d body %s code = %q, want %q", tc.status, tc.body, got, tc.want)
		}
	}
}

// TestProvingCompositeIntegration proves the composite merges the scene and
// the source foreground exactly where the mask says, and refuses an invalid
// mask with provider_invalid_output instead of silently succeeding.
func TestProvingCompositeIntegration(t *testing.T) {
	fgRGBA := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	mask := image.NewAlpha(image.Rect(0, 0, 4, 4))
	scene := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			scene.Set(x, y, color.RGBA{R: 0, G: 0, B: 255, A: 255}) // scene background blue
			if y >= 2 {
				fgRGBA.Set(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255}) // product red
				mask.SetAlpha(x, y, color.Alpha{A: 255})
			}
		}
	}
	// A soft mask edge must preserve the source's straight RGB values while
	// applying the mask alpha once in the output.
	fgRGBA.SetNRGBA(0, 2, color.NRGBA{R: 200, G: 40, B: 20, A: 255})
	mask.SetAlpha(0, 2, color.Alpha{A: 128})
	composed, err := provingComposite(
		Foreground{ContentType: "image/png", Bytes: encodeTestPNG(t, fgRGBA), MaskBytes: encodeTestPNG(t, mask)},
		ComposedImage{ContentType: "image/png", Bytes: encodeTestPNG(t, scene)},
	)
	if err != nil {
		t.Fatalf("valid composite: %v", err)
	}
	result, _, err := image.Decode(bytes.NewReader(composed.Bytes))
	if err != nil {
		t.Fatalf("decode composite: %v", err)
	}
	// Background pixels must carry the scene color; product pixels the source color.
	if r, g, b, a := result.At(0, 0).RGBA(); !(r == 0 && g == 0 && b == 0xffff && a == 0xffff) {
		t.Fatalf("composite background = %+v, want scene blue kept", result.At(0, 0))
	}
	if r, g, b, _ := result.At(0, 3).RGBA(); !(r == 0xffff && g == 0 && b == 0) {
		t.Fatalf("composite product = %+v, want source red kept", result.At(0, 3))
	}
	softEdge := color.NRGBAModel.Convert(result.At(0, 2)).(color.NRGBA)
	if softEdge != (color.NRGBA{R: 200, G: 40, B: 20, A: 128}) {
		t.Fatalf("composite soft edge = %+v, want source RGB with mask alpha", softEdge)
	}

	// A different scene size: compositing cannot be proven.
	wrongScene := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			wrongScene.Set(x, y, scene.At(0, 0))
		}
	}
	if _, err := provingComposite(
		Foreground{ContentType: "image/png", Bytes: encodeTestPNG(t, fgRGBA), MaskBytes: encodeTestPNG(t, mask)},
		ComposedImage{ContentType: "image/png", Bytes: encodeTestPNG(t, wrongScene)},
	); err == nil {
		t.Fatalf("size mismatch must be refused")
	} else if got := failureCode(err); got != ProviderFailureInvalidOutput {
		t.Fatalf("size mismatch code = %q", got)
	}

	// A fully opaque mask proves nothing.
	opaque := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			opaque.SetAlpha(x, y, color.Alpha{A: 255})
		}
	}
	if _, err := provingComposite(
		Foreground{ContentType: "image/png", Bytes: encodeTestPNG(t, fgRGBA), MaskBytes: encodeTestPNG(t, opaque)},
		ComposedImage{ContentType: "image/png", Bytes: encodeTestPNG(t, scene)},
	); err == nil {
		t.Fatalf("opaque mask must be refused")
	} else if got := failureCode(err); got != ProviderFailureInvalidOutput {
		t.Fatalf("opaque mask code = %q", got)
	}
	transparent := image.NewAlpha(image.Rect(0, 0, 4, 4))
	if _, err := provingComposite(
		Foreground{ContentType: "image/png", Bytes: encodeTestPNG(t, fgRGBA), MaskBytes: encodeTestPNG(t, transparent)},
		ComposedImage{ContentType: "image/png", Bytes: encodeTestPNG(t, scene)},
	); failureCode(err) != ProviderFailureInvalidOutput {
		t.Fatalf("transparent mask code = %q, want %q", failureCode(err), ProviderFailureInvalidOutput)
	}
	leakingForeground := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(leakingForeground, leakingForeground.Bounds(), fgRGBA, image.Point{}, draw.Src)
	leakingForeground.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	if _, err := provingComposite(
		Foreground{ContentType: "image/png", Bytes: encodeTestPNG(t, leakingForeground), MaskBytes: encodeTestPNG(t, mask)},
		ComposedImage{ContentType: "image/png", Bytes: encodeTestPNG(t, scene)},
	); failureCode(err) != ProviderFailureInvalidOutput {
		t.Fatalf("foreground leak code = %q, want %q", failureCode(err), ProviderFailureInvalidOutput)
	}
}

// TestAdapterAuthorizationNotLogged documents that the composer writes no
// bearer material into any returned value or error string.
func TestAdapterAuthorizationNotLogged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		if _, err := w.Write([]byte("access denied: secret-token")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer server.Close()
	adapter := NewOpenAIImageComposer(server.URL, "secret-token", "mdl", 0)
	composed, err := adapter.ComposeScene(context.Background(), SceneRequest{SceneKey: "k", Foreground: validAlphaForegroundForCompose(t)})
	server.Close()
	if err == nil {
		t.Fatalf("refused response must fail")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(composed.ContentType, "secret-token") {
		t.Fatalf("error must not carry the bearer material: %v", err)
	}
}

func validAlphaForegroundForCompose(t *testing.T) Foreground {
	t.Helper()
	buf := bytes.Buffer{}
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if y >= 2 {
				img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
			}
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fg: %v", err)
	}
	mask := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			mask.SetAlpha(x, y, color.Alpha{A: 255})
		}
	}
	mbuf := bytes.Buffer{}
	if err := png.Encode(&mbuf, mask); err != nil {
		t.Fatalf("encode mask: %v", err)
	}
	return Foreground{ContentType: "image/png", Bytes: buf.Bytes(), MaskBytes: mbuf.Bytes()}
}

func writeImageB64Response(t *testing.T, w http.ResponseWriter, data []byte) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(data) + `"}]}`))
}

func writeImageURLResponse(t *testing.T, w http.ResponseWriter, url string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":[{"url":"` + url + `"}]}`))
}

func readRequestBodyString(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}
