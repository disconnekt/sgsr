package sgsr

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"embed"
	"io"
	"io/fs"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gofiber/fiber/v2"
	"github.com/klauspost/compress/zstd"
)

//go:embed testdata/static
var embeddedStaticFS embed.FS

func TestRegisterEmbeddedStatic_Validation(t *testing.T) {
	err := RegisterEmbeddedStatic(nil, "/assets", embeddedStaticFS, "testdata/static")
	if err == nil || err.Error() != "router cannot be nil" {
		t.Fatalf("unexpected error: %v", err)
	}

	app := fiber.New()

	err = RegisterEmbeddedStatic(app, "", embeddedStaticFS, "testdata/static")
	if err == nil || err.Error() != "prefix cannot be empty" {
		t.Fatalf("unexpected error: %v", err)
	}

	err = RegisterEmbeddedStatic(app, "/assets/*", embeddedStaticFS, "testdata/static")
	if err == nil || err.Error() != "prefix cannot contain wildcard" {
		t.Fatalf("unexpected error: %v", err)
	}

	err = RegisterEmbeddedStatic(app, "/assets", nil, "testdata/static")
	if err == nil || err.Error() != "filesystem cannot be nil" {
		t.Fatalf("unexpected error: %v", err)
	}

	err = RegisterEmbeddedStatic(app, "/assets", embeddedStaticFS, "missing")
	if err == nil || (!strings.Contains(err.Error(), "failed to open static directory") &&
		!strings.Contains(err.Error(), "failed to preload static assets")) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterEmbeddedStatic_ServesCompressedVariants(t *testing.T) {
	app := fiber.New()
	if err := RegisterEmbeddedStatic(app, "/assets", embeddedStaticFS, "testdata/static"); err != nil {
		t.Fatalf("failed to register static assets: %v", err)
	}

	expected, err := fs.ReadFile(embeddedStaticFS, "testdata/static/repeat.txt")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	tests := []struct {
		name             string
		acceptEncoding   string
		expectedEncoding string
	}{
		{
			name:             "identity by default",
			acceptEncoding:   "",
			expectedEncoding: "",
		},
		{
			name:             "gzip",
			acceptEncoding:   "gzip",
			expectedEncoding: ContentEncodingGzip,
		},
		{
			name:             "deflate",
			acceptEncoding:   "deflate",
			expectedEncoding: ContentEncodingDeflate,
		},
		{
			name:             "brotli",
			acceptEncoding:   "br",
			expectedEncoding: ContentEncodingBrotli,
		},
		{
			name:             "zstd",
			acceptEncoding:   "zstd",
			expectedEncoding: ContentEncodingZstd,
		},
		{
			name:             "quality preference",
			acceptEncoding:   "gzip;q=0.5, br;q=0.9",
			expectedEncoding: ContentEncodingBrotli,
		},
		{
			name:             "explicit identity preference",
			acceptEncoding:   "identity;q=1, br;q=0.4",
			expectedEncoding: "",
		},
		{
			name:             "wildcard falls to server preference",
			acceptEncoding:   "*;q=1",
			expectedEncoding: ContentEncodingZstd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(fiber.MethodGet, "/assets/repeat.txt", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set(fiber.HeaderAcceptEncoding, tt.acceptEncoding)
			}

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("unexpected status: %d", resp.StatusCode)
			}

			gotEncoding := resp.Header.Get(fiber.HeaderContentEncoding)
			if gotEncoding != tt.expectedEncoding {
				t.Fatalf("expected encoding %q, got %q", tt.expectedEncoding, gotEncoding)
			}

			compressedBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}

			body, err := decompressBody(tt.expectedEncoding, compressedBody)
			if err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}

			if !bytes.Equal(body, expected) {
				t.Fatalf("unexpected body")
			}

			if !strings.Contains(resp.Header.Get(fiber.HeaderVary), fiber.HeaderAcceptEncoding) {
				t.Fatalf("expected Vary header to include Accept-Encoding")
			}
		})
	}
}

func TestRegisterEmbeddedStatic_SkipInefficientCompression(t *testing.T) {
	app := fiber.New()
	if err := RegisterEmbeddedStatic(app, "/assets", embeddedStaticFS, "testdata/static"); err != nil {
		t.Fatalf("failed to register static assets: %v", err)
	}

	req := httptest.NewRequest(fiber.MethodGet, "/assets/hello.txt", nil)
	req.Header.Set(fiber.HeaderAcceptEncoding, "gzip")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if got := resp.Header.Get(fiber.HeaderContentEncoding); got != "" {
		t.Fatalf("expected identity response for tiny asset, got %q", got)
	}

	req = httptest.NewRequest(fiber.MethodGet, "/assets/hello.txt", nil)
	req.Header.Set(fiber.HeaderAcceptEncoding, "gzip;q=1, identity;q=0, *;q=0")

	resp2, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != fiber.StatusNotAcceptable {
		t.Fatalf("expected 406, got %d", resp2.StatusCode)
	}
}

func TestRegisterEmbeddedStatic_NotAcceptable(t *testing.T) {
	app := fiber.New()
	if err := RegisterEmbeddedStatic(app, "/assets", embeddedStaticFS, "testdata/static"); err != nil {
		t.Fatalf("failed to register static assets: %v", err)
	}

	req := httptest.NewRequest(fiber.MethodGet, "/assets/hello.txt", nil)
	req.Header.Set(fiber.HeaderAcceptEncoding, "identity;q=0, br;q=0, gzip;q=0, deflate;q=0, zstd;q=0")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNotAcceptable {
		t.Fatalf("expected 406, got %d", resp.StatusCode)
	}
}

func TestRegisterEmbeddedStatic_IndexAnd404(t *testing.T) {
	app := fiber.New()
	if err := RegisterEmbeddedStatic(app, "/assets", embeddedStaticFS, "testdata/static"); err != nil {
		t.Fatalf("failed to register static assets: %v", err)
	}

	paths := map[string]string{
		"/assets/":      "<h1>root index</h1>\n",
		"/assets/docs":  "<h1>docs index</h1>\n",
		"/assets/docs/": "<h1>docs index</h1>\n",
	}

	for requestPath, expectedBody := range paths {
		func() {
			req := httptest.NewRequest(fiber.MethodGet, requestPath, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed for %q: %v", requestPath, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("unexpected status for %q: %d", requestPath, resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read body for %q: %v", requestPath, err)
			}
			if string(body) != expectedBody {
				t.Fatalf("unexpected body for %q: %q", requestPath, string(body))
			}
		}()
	}

	req := httptest.NewRequest(fiber.MethodGet, "/assets/missing.txt", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAppRegisterEmbeddedStatic(t *testing.T) {
	fiberApp := fiber.New()
	cfg := NewConfig(NewLogger(), fiberApp, ":8080")
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if err := app.RegisterEmbeddedStatic("/assets", embeddedStaticFS, "testdata/static"); err != nil {
		t.Fatalf("failed to register embedded static on App: %v", err)
	}

	req := httptest.NewRequest(fiber.MethodGet, "/assets/hello.txt", nil)
	resp, err := fiberApp.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func decompressBody(encoding string, data []byte) ([]byte, error) {
	switch encoding {
	case "":
		return data, nil
	case ContentEncodingGzip:
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	case ContentEncodingDeflate:
		reader := flate.NewReader(bytes.NewReader(data))
		defer reader.Close()
		return io.ReadAll(reader)
	case ContentEncodingBrotli:
		return io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
	case ContentEncodingZstd:
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		return decoder.DecodeAll(data, nil)
	default:
		return nil, io.ErrUnexpectedEOF
	}
}

func TestNormalizeEncodingOrder(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []string
		wantErr bool
	}{
		{
			name:  "auto-appends missing identity",
			input: []string{"gzip", "br"},
			want:  []string{"gzip", "br", ContentEncodingIdentity},
		},
		{
			name:  "preserves explicit identity position",
			input: []string{ContentEncodingIdentity, "gzip"},
			want:  []string{ContentEncodingIdentity, "gzip"},
		},
		{
			name:  "deduplicates repeated encoding",
			input: []string{"gzip", "gzip", "br"},
			want:  []string{"gzip", "br", ContentEncodingIdentity},
		},
		{
			name:  "normalizes x-gzip alias",
			input: []string{"x-gzip"},
			want:  []string{ContentEncodingGzip, ContentEncodingIdentity},
		},
		{
			name:  "normalizes x-deflate alias",
			input: []string{"x-deflate"},
			want:  []string{ContentEncodingDeflate, ContentEncodingIdentity},
		},
		{
			name:    "rejects unknown encoding",
			input:   []string{"fake"},
			wantErr: true,
		},
		{
			name:    "rejects empty input",
			input:   []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeEncodingOrder(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegisterEmbeddedStatic_TooManyOptions(t *testing.T) {
	app := fiber.New()
	err := RegisterEmbeddedStatic(app, "/assets", embeddedStaticFS, "testdata/static",
		EmbeddedStaticOptions{}, EmbeddedStaticOptions{})
	if err == nil || err.Error() != "only one options argument is allowed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterEmbeddedStatic_CustomOptions(t *testing.T) {
	app := fiber.New()
	err := RegisterEmbeddedStatic(app, "/assets", embeddedStaticFS, "testdata/static",
		EmbeddedStaticOptions{
			IndexFile:    "index.html",
			CacheControl: "max-age=3600",
			Encodings:    []string{"gzip", "br"},
		})
	if err != nil {
		t.Fatalf("failed to register with custom options: %v", err)
	}

	req := httptest.NewRequest(fiber.MethodGet, "/assets/repeat.txt", nil)
	req.Header.Set(fiber.HeaderAcceptEncoding, "gzip")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if got := resp.Header.Get(fiber.HeaderCacheControl); got != "max-age=3600" {
		t.Fatalf("expected Cache-Control %q, got %q", "max-age=3600", got)
	}
	if got := resp.Header.Get(fiber.HeaderContentEncoding); got != ContentEncodingGzip {
		t.Fatalf("expected encoding %q, got %q", ContentEncodingGzip, got)
	}

	// zstd should not be offered (not in custom Encodings)
	req2 := httptest.NewRequest(fiber.MethodGet, "/assets/repeat.txt", nil)
	req2.Header.Set(fiber.HeaderAcceptEncoding, "zstd")

	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()

	// zstd not in custom encodings — server falls back to identity
	if got := resp2.Header.Get(fiber.HeaderContentEncoding); got != "" {
		t.Fatalf("expected identity (no encoding header), got %q", got)
	}
}
