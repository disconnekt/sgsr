package sgsr

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"embed"
	"io"
	"net/http/httptest"
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

	expected := "Hello from embedded static file.\n"

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
			name:             "wildcard falls to server preference",
			acceptEncoding:   "*;q=1",
			expectedEncoding: ContentEncodingZstd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(fiber.MethodGet, "/assets/hello.txt", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set(fiber.HeaderAcceptEncoding, tt.acceptEncoding)
			}

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
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

			if string(body) != expected {
				t.Fatalf("unexpected body: %q", string(body))
			}

			if !strings.Contains(resp.Header.Get(fiber.HeaderVary), fiber.HeaderAcceptEncoding) {
				t.Fatalf("expected Vary header to include Accept-Encoding")
			}
		})
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
		req := httptest.NewRequest(fiber.MethodGet, requestPath, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed for %q: %v", requestPath, err)
		}
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
	}

	req := httptest.NewRequest(fiber.MethodGet, "/assets/missing.txt", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
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
