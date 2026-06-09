package output

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/imageutil"
	"github.com/walker1211/news-briefing/internal/logutil"
	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/statefile"
	_ "golang.org/x/image/webp"
)

type markdownImageDownloader func(rawURL string, path string) error

const maxMarkdownImageBytes = 30 * 1024 * 1024
const maxMarkdownImageOutputBytes = 5 * 1024 * 1024
const minMarkdownImageWidth = 32
const minMarkdownImageHeight = 32
const markdownImageDownloadAttempts = 3
const markdownImageRetryDelay = 200 * time.Millisecond
const markdownImageResizePercent = 80

func WriteMarkdown(briefing *model.Briefing, outputDir string) (string, error) {
	return writeMarkdownWithImageDownloader(briefing, outputDir, downloadMarkdownImage)
}

func writeMarkdownWithImageDownloader(briefing *model.Briefing, outputDir string, download markdownImageDownloader) (string, error) {
	filename := briefingFileName(briefing.Date, briefing.Period)
	path := filepath.Join(outputDir, filename)

	rawContent := briefing.RawContent
	if download != nil {
		rawContent = localizeMarkdownImages(rawContent, outputDir, briefing.Date, briefing.Period, download)
	}
	content := briefingHeaderBlock(briefing) + rawContent

	if err := statefile.WriteAtomicReplaceOnly(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write markdown: %w", err)
	}

	return path, nil
}

func localizeMarkdownImages(rawContent string, outputDir string, date string, period string, download markdownImageDownloader) string {
	assetDirName := briefingAssetDirName(date, period)
	assetDir := filepath.Join(outputDir, "assets", assetDirName)
	assetRelDir := filepath.ToSlash(filepath.Join("assets", assetDirName))
	lines := strings.Split(rawContent, "\n")
	for i, line := range lines {
		alt, imageURL, ok := parseMarkdownImage(line)
		imageURL = html.UnescapeString(imageURL)
		if !ok || !isRemoteMarkdownImage(imageURL) {
			continue
		}
		if imageutil.IsTrackingImageURL(imageURL) {
			lines[i] = ""
			continue
		}
		filename := markdownImageFileName(imageURL)
		localPath := filepath.Join(assetDir, filename)
		if err := os.MkdirAll(assetDir, 0o755); err != nil {
			continue
		}
		if err := download(imageURL, localPath); err != nil {
			logutil.Warnf("Markdown image download failed: host=%s error=%v", markdownImageHost(imageURL), err)
			lines[i] = ""
			continue
		}
		lines[i] = fmt.Sprintf("![%s](%s)", alt, filepath.ToSlash(filepath.Join(assetRelDir, filename)))
	}
	return strings.Join(lines, "\n")
}

func briefingAssetDirName(date string, period string) string {
	return fmt.Sprintf("%s-%s", date, period)
}

func markdownImageFileName(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	hash := hex.EncodeToString(sum[:])[:16]
	return hash + markdownImageExtension(rawURL)
}

func markdownImageExtension(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ".jpg"
	}
	switch ext := strings.ToLower(filepath.Ext(parsed.Path)); ext {
	case ".jpg", ".jpeg", ".png", ".gif":
		return ext
	case ".webp", ".avif":
		return ".jpg"
	default:
		return ".jpg"
	}
}

func isRemoteMarkdownImage(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func markdownImageHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func embeddedMarkdownImageURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	candidate := html.UnescapeString(strings.TrimSpace(parsed.Query().Get("url")))
	if !isRemoteMarkdownImage(candidate) || !hasKnownMarkdownImageExtension(candidate) {
		return ""
	}
	return candidate
}

func hasKnownMarkdownImageExtension(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(filepath.Ext(parsed.Path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		return true
	default:
		return false
	}
}

func downloadMarkdownImage(rawURL string, path string) error {
	rawURL = html.UnescapeString(strings.TrimSpace(rawURL))
	if !isRemoteMarkdownImage(rawURL) {
		return fmt.Errorf("unsupported image URL: %s", rawURL)
	}
	client := http.Client{Timeout: 15 * time.Second}
	if err := downloadMarkdownImageWithAttempts(client, rawURL, path); err != nil {
		fallbackURL := embeddedMarkdownImageURL(rawURL)
		if fallbackURL == "" || fallbackURL == rawURL {
			return err
		}
		if fallbackErr := downloadMarkdownImageWithAttempts(client, fallbackURL, path); fallbackErr != nil {
			return fmt.Errorf("%w; fallback host=%s error=%v", err, markdownImageHost(fallbackURL), fallbackErr)
		}
	}
	return nil
}

func downloadMarkdownImageWithAttempts(client http.Client, rawURL string, path string) error {
	var lastErr error
	for attempt := 1; attempt <= markdownImageDownloadAttempts; attempt++ {
		if err := downloadMarkdownImageOnce(client, rawURL, path); err != nil {
			lastErr = err
			if attempt < markdownImageDownloadAttempts {
				time.Sleep(markdownImageRetryDelay)
			}
			continue
		}
		return nil
	}
	return lastErr
}

func downloadMarkdownImageOnce(client http.Client, rawURL string, path string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "image/jpeg,image/png,image/gif,image/*,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download image: %s", resp.Status)
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if contentType != "" && !strings.HasPrefix(contentType, "image/") && !isImageLikeOctetStream(contentType, rawURL) {
		return fmt.Errorf("download image: content type %s", contentType)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMarkdownImageBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxMarkdownImageBytes {
		return fmt.Errorf("download image: file too large")
	}
	return writeValidatedMarkdownImage(path, data)
}

func writeValidatedMarkdownImage(path string, data []byte) error {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("download image: decode config: %w", err)
	}
	if config.Width < minMarkdownImageWidth || config.Height < minMarkdownImageHeight {
		return fmt.Errorf("download image: dimensions %dx%d too small", config.Width, config.Height)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if markdownImageFormatMatchesExtension(format, ext) && len(data) <= maxMarkdownImageOutputBytes {
		return statefile.WriteAtomicReplaceOnly(path, data, 0o644)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("download image: decode: %w", err)
	}
	switch ext {
	case ".jpg", ".jpeg":
		if len(data) > maxMarkdownImageOutputBytes {
			return writeCompressedJPEGMarkdownImage(path, img)
		}
		return writeJPEGMarkdownImage(path, img)
	case ".png":
		if len(data) > maxMarkdownImageOutputBytes {
			return writeCompressedPNGMarkdownImage(path, img)
		}
		return writePNGMarkdownImage(path, img)
	default:
		return fmt.Errorf("download image: format %s does not match extension %s", format, ext)
	}
}

func markdownImageFormatMatchesExtension(format string, ext string) bool {
	switch format {
	case "jpeg":
		return ext == ".jpg" || ext == ".jpeg"
	case "png":
		return ext == ".png"
	case "gif":
		return ext == ".gif"
	default:
		return false
	}
}

func writeJPEGMarkdownImage(path string, img image.Image) error {
	data, err := encodeJPEGMarkdownImage(img, 90)
	if err != nil {
		return err
	}
	return statefile.WriteAtomicReplaceOnly(path, data, 0o644)
}

func writeCompressedJPEGMarkdownImage(path string, img image.Image) error {
	data, err := compressedMarkdownImageBytes(img, func(candidate image.Image) ([]byte, error) {
		return encodeJPEGMarkdownImage(candidate, 85)
	})
	if err != nil {
		return err
	}
	return statefile.WriteAtomicReplaceOnly(path, data, 0o644)
}

func writePNGMarkdownImage(path string, img image.Image) error {
	data, err := encodePNGMarkdownImage(img)
	if err != nil {
		return err
	}
	return statefile.WriteAtomicReplaceOnly(path, data, 0o644)
}

func writeCompressedPNGMarkdownImage(path string, img image.Image) error {
	data, err := compressedMarkdownImageBytes(img, encodePNGMarkdownImage)
	if err != nil {
		return err
	}
	return statefile.WriteAtomicReplaceOnly(path, data, 0o644)
}

func encodeJPEGMarkdownImage(img image.Image, quality int) ([]byte, error) {
	var out bytes.Buffer
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Over)
	if err := jpeg.Encode(&out, rgba, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func encodePNGMarkdownImage(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func compressedMarkdownImageBytes(img image.Image, encode func(image.Image) ([]byte, error)) ([]byte, error) {
	candidate := img
	for {
		data, err := encode(candidate)
		if err != nil {
			return nil, err
		}
		if len(data) <= maxMarkdownImageOutputBytes {
			return data, nil
		}
		width, height, ok := nextMarkdownImageSize(candidate.Bounds())
		if !ok {
			return nil, fmt.Errorf("download image: compressed file too large")
		}
		candidate = resizeMarkdownImage(candidate, width, height)
	}
}

func nextMarkdownImageSize(bounds image.Rectangle) (int, int, bool) {
	width := bounds.Dx()
	height := bounds.Dy()
	nextWidth := width
	nextHeight := height
	if width > minMarkdownImageWidth {
		nextWidth = width * markdownImageResizePercent / 100
		if nextWidth < minMarkdownImageWidth {
			nextWidth = minMarkdownImageWidth
		}
	}
	if height > minMarkdownImageHeight {
		nextHeight = height * markdownImageResizePercent / 100
		if nextHeight < minMarkdownImageHeight {
			nextHeight = minMarkdownImageHeight
		}
	}
	if nextWidth == width && nextHeight == height {
		return width, height, false
	}
	return nextWidth, nextHeight, true
}

func resizeMarkdownImage(src image.Image, width int, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := src.Bounds()
	for y := range height {
		sy := bounds.Min.Y + y*bounds.Dy()/height
		for x := range width {
			sx := bounds.Min.X + x*bounds.Dx()/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func isImageLikeOctetStream(contentType string, rawURL string) bool {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/octet-stream") {
		return false
	}
	switch markdownImageExtension(rawURL) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		return true
	default:
		return false
	}
}

func WriteDeepDive(topic, content, outputDir string, date string) (string, error) {
	deepDir := filepath.Join(outputDir, "deep")
	filename := fmt.Sprintf("%s-%s.md", date, sanitize(topic))
	path := filepath.Join(deepDir, filename)

	header := fmt.Sprintf("# 话题深挖包：%s\n\n", topic)
	full := header + content

	if err := statefile.WriteAtomicReplaceOnly(path, []byte(full), 0644); err != nil {
		return "", fmt.Errorf("write deep dive: %w", err)
	}

	return path, nil
}

func sanitize(s string) string {
	result := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else if c == ' ' {
			result = append(result, '-')
		}
	}
	if len(result) > 50 {
		result = result[:50]
	}
	return string(result)
}
