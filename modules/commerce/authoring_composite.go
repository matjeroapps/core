package commerce

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	// Keep JPEG decoding available for provider-provided layers that arrive in
	// JPEG; outputs are always re-encoded as PNG.
	_ "image/jpeg"
)

// provingComposite composites the isolated product foreground onto a
// provider-generated surrounding scene in Core, and refuses the output unless
// the compositing can be proven to preserve the source product pixels:
//
//   - the mask (alpha channel) must define a product region: it has both an
//     opaque product area and a transparent background area;
//   - outside the product region the foreground must be fully transparent, so
//     no redrawn product pixels can leak in;
//   - the scene must carry the exact same dimensions, so compositing is a
//     per-pixel paste of source pixels and never a re-render;
//   - anything undecodable proves nothing and is refused.
//
// An unverifiable composite returns ProviderFailureInvalidOutput; no partial
// result is written for the candidate.
func provingComposite(foreground Foreground, scene GeneratedProductImage) (GeneratedProductImage, error) {
	fg, _, err := image.Decode(bytes.NewReader(foreground.Bytes))
	if err != nil {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	mask, _, err := image.Decode(bytes.NewReader(foreground.MaskBytes))
	if err != nil {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	sceneImg, _, err := image.Decode(bytes.NewReader(scene.Bytes))
	if err != nil {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}

	fgBounds := fg.Bounds()
	maskBounds := mask.Bounds()
	if fgBounds.Size() != maskBounds.Size() || maskBounds.Size() != sceneImg.Bounds().Size() {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}

	fgRGBA := image.NewRGBA(fgBounds)
	draw.Draw(fgRGBA, fgBounds, fg, fgBounds.Min, draw.Src)
	maskAlpha := image.NewAlpha(maskBounds)
	draw.Draw(maskAlpha, maskBounds, mask, maskBounds.Min, draw.Src)

	// Verify the mask defines both regions and the foreground is fully
	// transparent outside the product region.
	fgMin, maskMin := fgBounds.Min, maskBounds.Min
	offX, offY := maskMin.X-fgMin.X, maskMin.Y-fgMin.Y
	hasProduct, hasBackground := false, false
	for y := maskBounds.Min.Y; y < maskBounds.Max.Y; y++ {
		for x := maskBounds.Min.X; x < maskBounds.Max.X; x++ {
			a := maskAlpha.AlphaAt(x, y).A
			if a == 0 {
				hasBackground = true
				if fgRGBA.RGBAAt(x-offX, y-offY).A != 0 {
					// Product pixels bleed outside the mask: the foreground
					// cannot be trusted to preserve the source image.
					return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
				}
			} else if a == 0xff {
				hasProduct = true
			}
		}
	}
	if !hasProduct || !hasBackground {
		// A fully opaque (or fully transparent) mask proves nothing about the
		// composite: refuse instead of silently succeeding.
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}

	// Normalize the scene onto a fresh RGBA canvas and keep the generated
	// scene pixels untouched outside the mask region.
	sceneRGBA := image.NewNRGBA(image.Rect(0, 0, sceneImg.Bounds().Dx(), sceneImg.Bounds().Dy()))
	draw.Draw(sceneRGBA, sceneRGBA.Bounds(), sceneImg, sceneImg.Bounds().Min, draw.Src)
	for y := maskBounds.Min.Y; y < maskBounds.Max.Y; y++ {
		for x := maskBounds.Min.X; x < maskBounds.Max.X; x++ {
			a := maskAlpha.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			src := color.NRGBAModel.Convert(fg.At(x-offX, y-offY)).(color.NRGBA)
			// Keep straight RGB in the NRGBA canvas so PNG encoding does not
			// round product colors when applying a partially transparent mask.
			sceneRGBA.SetNRGBA(x, y, color.NRGBA{R: src.R, G: src.G, B: src.B, A: a})
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, sceneRGBA); err != nil {
		return GeneratedProductImage{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	return GeneratedProductImage{ContentType: "image/png", Bytes: out.Bytes()}, nil
}
