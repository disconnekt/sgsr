package sgsr

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gofiber/fiber/v2"
	"github.com/klauspost/compress/zstd"
)

const (
	// ContentEncodingIdentity means no content coding is applied.
	ContentEncodingIdentity = "identity"
	// ContentEncodingGzip means gzip content coding is applied.
	ContentEncodingGzip = "gzip"
	// ContentEncodingDeflate means deflate content coding is applied.
	ContentEncodingDeflate = "deflate"
	// ContentEncodingBrotli means brotli content coding is applied.
	ContentEncodingBrotli = "br"
	// ContentEncodingZstd means zstd content coding is applied.
	ContentEncodingZstd = "zstd"
)

var defaultStaticEncodings = []string{
	ContentEncodingZstd,
	ContentEncodingBrotli,
	ContentEncodingGzip,
	ContentEncodingDeflate,
	ContentEncodingIdentity,
}

// EmbeddedStaticOptions configures embedded static handler behavior.
type EmbeddedStaticOptions struct {
	// IndexFile is used when requested path points to a directory.
	// Default: index.html
	IndexFile string
	// CacheControl is an optional Cache-Control response header value.
	CacheControl string
	// Encodings defines server-side preferred encoding order.
	// Supported values: zstd, br, gzip, deflate, identity.
	// If empty, all supported encodings are pre-built and enabled.
	Encodings []string
}

type embeddedStaticAsset struct {
	contentType string
	variants    map[string][]byte
}

type embeddedStaticHandler struct {
	prefix       string
	indexFile    string
	cacheControl string
	encodings    []string
	assets       map[string]embeddedStaticAsset
}

// RegisterEmbeddedStatic registers a static handler backed by embed-compatible fs.FS.
// Files are pre-compressed during registration for all selected encodings.
func RegisterEmbeddedStatic(router fiber.Router, prefix string, staticFS fs.FS, dir string, opts ...EmbeddedStaticOptions) error {
	if router == nil {
		return errors.New("router cannot be nil")
	}
	if staticFS == nil {
		return errors.New("filesystem cannot be nil")
	}
	if len(opts) > 1 {
		return errors.New("only one options argument is allowed")
	}

	cfg, err := newEmbeddedStaticOptions(opts)
	if err != nil {
		return err
	}

	normalizedPrefix, err := normalizeRoutePrefix(prefix)
	if err != nil {
		return err
	}

	sourceFS, err := subFS(staticFS, dir)
	if err != nil {
		return err
	}

	assets, err := preloadEmbeddedAssets(sourceFS, cfg.encodings)
	if err != nil {
		return err
	}

	handler := &embeddedStaticHandler{
		prefix:       normalizedPrefix,
		indexFile:    cfg.indexFile,
		cacheControl: cfg.cacheControl,
		encodings:    cfg.encodings,
		assets:       assets,
	}

	for _, route := range staticRoutes(normalizedPrefix) {
		router.Get(route, handler.serve)
		router.Head(route, handler.serve)
	}

	return nil
}

// RegisterEmbeddedStatic registers a static handler on the underlying fiber app.
func (a *App) RegisterEmbeddedStatic(prefix string, staticFS fs.FS, dir string, opts ...EmbeddedStaticOptions) error {
	if a == nil {
		return errors.New("app cannot be nil")
	}
	return RegisterEmbeddedStatic(a.cfg.app, prefix, staticFS, dir, opts...)
}

type embeddedStaticOptions struct {
	indexFile    string
	cacheControl string
	encodings    []string
}

func newEmbeddedStaticOptions(opts []EmbeddedStaticOptions) (embeddedStaticOptions, error) {
	cfg := embeddedStaticOptions{
		indexFile: "index.html",
		encodings: append([]string(nil), defaultStaticEncodings...),
	}

	if len(opts) == 0 {
		return cfg, nil
	}

	if opts[0].IndexFile != "" {
		cfg.indexFile = strings.TrimPrefix(path.Clean("/"+opts[0].IndexFile), "/")
	}
	cfg.cacheControl = opts[0].CacheControl

	if len(opts[0].Encodings) == 0 {
		return cfg, nil
	}

	encodings, err := normalizeEncodingOrder(opts[0].Encodings)
	if err != nil {
		return embeddedStaticOptions{}, err
	}
	cfg.encodings = encodings
	return cfg, nil
}

func normalizeRoutePrefix(prefix string) (string, error) {
	cleanPrefix := strings.TrimSpace(prefix)
	if cleanPrefix == "" {
		return "", errors.New("prefix cannot be empty")
	}
	if strings.Contains(cleanPrefix, "*") {
		return "", errors.New("prefix cannot contain wildcard")
	}
	if !strings.HasPrefix(cleanPrefix, "/") {
		cleanPrefix = "/" + cleanPrefix
	}
	cleanPrefix = path.Clean(cleanPrefix)
	if cleanPrefix == "." {
		cleanPrefix = "/"
	}
	return cleanPrefix, nil
}

func subFS(staticFS fs.FS, dir string) (fs.FS, error) {
	if dir == "" || dir == "." {
		return staticFS, nil
	}
	sourceFS, err := fs.Sub(staticFS, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open static directory %q: %w", dir, err)
	}
	return sourceFS, nil
}

func preloadEmbeddedAssets(sourceFS fs.FS, encodings []string) (map[string]embeddedStaticAsset, error) {
	assets := make(map[string]embeddedStaticAsset)

	compressors, cleanup, err := prepareCompressors(encodings)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	walkErr := fs.WalkDir(sourceFS, ".", func(file string, entry fs.DirEntry, dirErr error) error {
		if dirErr != nil {
			return dirErr
		}
		if entry.IsDir() {
			return nil
		}

		raw, err := fs.ReadFile(sourceFS, file)
		if err != nil {
			return err
		}
		key := "/" + strings.TrimPrefix(path.Clean("/"+filepathToURLPath(file)), "/")
		asset, err := buildAsset(raw, file, encodings, compressors)
		if err != nil {
			return err
		}
		assets[key] = asset
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("failed to preload static assets: %w", walkErr)
	}
	if len(assets) == 0 {
		return nil, errors.New("no static files found")
	}
	return assets, nil
}

func buildAsset(raw []byte, file string, encodings []string, compressors map[string]func([]byte) ([]byte, error)) (embeddedStaticAsset, error) {
	variants := make(map[string][]byte, len(encodings))
	variants[ContentEncodingIdentity] = raw

	for _, encoding := range encodings {
		if encoding == ContentEncodingIdentity {
			continue
		}
		compress, ok := compressors[encoding]
		if !ok {
			return embeddedStaticAsset{}, fmt.Errorf("unsupported compression encoding %q", encoding)
		}
		compressed, err := compress(raw)
		if err != nil {
			return embeddedStaticAsset{}, fmt.Errorf("failed to compress %q with %s: %w", file, encoding, err)
		}
		// Keep only effective variants to reduce startup memory footprint.
		if len(compressed) >= len(raw) {
			continue
		}
		variants[encoding] = compressed
	}

	contentType := mime.TypeByExtension(path.Ext(file))
	if contentType == "" {
		contentType = http.DetectContentType(raw)
	}

	return embeddedStaticAsset{
		contentType: contentType,
		variants:    variants,
	}, nil
}

func prepareCompressors(encodings []string) (map[string]func([]byte) ([]byte, error), func(), error) {
	compressors := make(map[string]func([]byte) ([]byte, error))

	var zstdEncoder *zstd.Encoder
	needsZstd := false
	for _, encoding := range encodings {
		if encoding == ContentEncodingZstd {
			needsZstd = true
			break
		}
	}
	if needsZstd {
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create zstd encoder: %w", err)
		}
		zstdEncoder = encoder
	}

	compressors[ContentEncodingGzip] = compressGzip
	compressors[ContentEncodingDeflate] = compressDeflate
	compressors[ContentEncodingBrotli] = compressBrotli
	if zstdEncoder != nil {
		compressors[ContentEncodingZstd] = func(raw []byte) ([]byte, error) {
			return zstdEncoder.EncodeAll(raw, make([]byte, 0, len(raw))), nil
		}
	}

	cleanup := func() {
		if zstdEncoder != nil {
			zstdEncoder.Close()
		}
	}
	return compressors, cleanup, nil
}

func compressGzip(raw []byte) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(raw)))
	writer, err := gzip.NewWriterLevel(buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(raw); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressDeflate(raw []byte) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(raw)))
	writer, err := flate.NewWriter(buf, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(raw); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressBrotli(raw []byte) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(raw)))
	writer := brotli.NewWriterLevel(buf, brotli.BestCompression)
	if _, err := writer.Write(raw); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *embeddedStaticHandler) serve(c *fiber.Ctx) error {
	asset, ok := h.resolveAsset(c)
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}

	encoding, acceptable := negotiateEncoding(c.Get(fiber.HeaderAcceptEncoding), h.encodings, asset.variants)
	if !acceptable {
		return c.SendStatus(fiber.StatusNotAcceptable)
	}

	body := asset.variants[encoding]
	c.Vary(fiber.HeaderAcceptEncoding)
	c.Set(fiber.HeaderContentType, asset.contentType)
	if h.cacheControl != "" {
		c.Set(fiber.HeaderCacheControl, h.cacheControl)
	}
	if encoding != ContentEncodingIdentity {
		c.Set(fiber.HeaderContentEncoding, encoding)
	}

	if c.Method() == fiber.MethodHead {
		c.Response().Header.SetContentLength(len(body))
		return nil
	}

	return c.Send(body)
}

func (h *embeddedStaticHandler) resolveAsset(c *fiber.Ctx) (embeddedStaticAsset, bool) {
	relative := c.Params("*")
	if relative == "" && h.prefix != "/" {
		relative = strings.TrimPrefix(c.Path(), h.prefix)
	}

	relative = strings.TrimPrefix(filepathToURLPath(relative), "/")
	if relative == "" {
		relative = h.indexFile
	}

	cleanPath := strings.TrimPrefix(path.Clean("/"+relative), "/")

	if asset, ok := h.assets["/"+cleanPath]; ok {
		return asset, true
	}

	// Fallback to index file for directory-like paths.
	if cleanPath != h.indexFile {
		indexPath := "/" + path.Join(cleanPath, h.indexFile)
		if asset, ok := h.assets[indexPath]; ok {
			return asset, true
		}
	}

	return embeddedStaticAsset{}, false
}

func staticRoutes(prefix string) []string {
	if prefix == "/" {
		return []string{"/", "/*"}
	}
	return []string{prefix, prefix + "/*"}
}

func normalizeEncodingOrder(encodings []string) ([]string, error) {
	normalized := make([]string, 0, len(encodings)+1)
	seen := make(map[string]struct{})

	for _, raw := range encodings {
		encoding, err := canonicalEncoding(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[encoding]; exists {
			continue
		}
		seen[encoding] = struct{}{}
		normalized = append(normalized, encoding)
	}

	if len(normalized) == 0 {
		return nil, errors.New("encodings cannot be empty")
	}
	if _, hasIdentity := seen[ContentEncodingIdentity]; !hasIdentity {
		normalized = append(normalized, ContentEncodingIdentity)
	}
	return normalized, nil
}

func canonicalEncoding(encoding string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(encoding))
	switch value {
	case ContentEncodingIdentity:
		return ContentEncodingIdentity, nil
	case ContentEncodingGzip, "x-gzip":
		return ContentEncodingGzip, nil
	case ContentEncodingDeflate, "x-deflate":
		return ContentEncodingDeflate, nil
	case ContentEncodingBrotli:
		return ContentEncodingBrotli, nil
	case ContentEncodingZstd:
		return ContentEncodingZstd, nil
	default:
		return "", fmt.Errorf("unsupported encoding %q", encoding)
	}
}

func negotiateEncoding(header string, preferred []string, variants map[string][]byte) (string, bool) {
	if len(preferred) == 0 || len(variants) == 0 {
		return "", false
	}

	specs := parseAcceptEncoding(header)

	bestEncoding := ""
	bestQ := -1.0

	// Prefer an explicit compressed representation when acceptable.
	for _, encoding := range preferred {
		if encoding == ContentEncodingIdentity {
			continue
		}
		if _, ok := variants[encoding]; !ok {
			continue
		}
		q := specs.qValueFor(encoding)
		if q <= 0 {
			continue
		}
		if q > bestQ {
			bestQ = q
			bestEncoding = encoding
		}
	}

	identityQ := specs.qValueFor(ContentEncodingIdentity)
	if specs.hasDeclared(ContentEncodingIdentity) && identityQ > bestQ {
		if _, ok := variants[ContentEncodingIdentity]; ok && identityQ > 0 {
			return ContentEncodingIdentity, true
		}
	}

	if bestEncoding != "" {
		return bestEncoding, true
	}

	if identityQ <= 0 {
		return "", false
	}
	if _, ok := variants[ContentEncodingIdentity]; !ok {
		return "", false
	}
	return ContentEncodingIdentity, true
}

type acceptEncodingSpecs struct {
	declared map[string]float64
	wildcard float64
	hasValue bool
	hasAny   bool
}

func (a acceptEncodingSpecs) hasDeclared(encoding string) bool {
	_, ok := a.declared[encoding]
	return ok
}

func parseAcceptEncoding(header string) acceptEncodingSpecs {
	header = strings.TrimSpace(header)
	specs := acceptEncodingSpecs{
		declared: make(map[string]float64),
		hasValue: header != "",
	}
	if header == "" {
		return specs
	}

	for _, part := range strings.Split(header, ",") {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}

		name := token
		q := 1.0
		if semi := strings.Index(token, ";"); semi != -1 {
			name = strings.TrimSpace(token[:semi])
			params := strings.Split(token[semi+1:], ";")
			for _, param := range params {
				kv := strings.SplitN(strings.TrimSpace(param), "=", 2)
				if len(kv) != 2 {
					continue
				}
				if strings.ToLower(strings.TrimSpace(kv[0])) != "q" {
					continue
				}
				parsedQ, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
				if err != nil {
					continue
				}
				switch {
				case parsedQ < 0:
					q = 0
				case parsedQ > 1:
					q = 1
				default:
					q = parsedQ
				}
			}
		}

		name = strings.ToLower(name)
		switch name {
		case "*":
			specs.wildcard = q
			specs.hasAny = true
		default:
			canonical, err := canonicalEncoding(name)
			if err != nil {
				continue
			}
			specs.declared[canonical] = q
		}
	}

	return specs
}

func (a acceptEncodingSpecs) qValueFor(encoding string) float64 {
	if !a.hasValue {
		if encoding == ContentEncodingIdentity {
			return 1
		}
		return 0
	}

	if q, ok := a.declared[encoding]; ok {
		return q
	}

	if encoding == ContentEncodingIdentity {
		if a.hasAny && a.wildcard == 0 {
			return 0
		}
		return 1
	}

	if a.hasAny {
		return a.wildcard
	}

	return 0
}

func filepathToURLPath(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}
