package output

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	localizedImages := map[string]string{}
	if download != nil {
		rawContent, localizedImages = localizeMarkdownImages(rawContent, outputDir, briefing.Date, briefing.Period, download)
	}
	content := briefingHeaderBlock(briefing) + rawContent

	if err := statefile.WriteAtomicReplaceOnly(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write markdown: %w", err)
	}
	if err := writeCardManifestSidecar(briefing, path, localizedImages); err != nil {
		return "", fmt.Errorf("write card manifest: %w", err)
	}

	return path, nil
}

func localizeMarkdownImages(rawContent string, outputDir string, date string, period string, download markdownImageDownloader) (string, map[string]string) {
	localized := make(map[string]string)
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
		relPath := filepath.ToSlash(filepath.Join(assetRelDir, filename))
		localized[imageURL] = relPath
		lines[i] = fmt.Sprintf("![%s](%s)", alt, relPath)
	}
	return strings.Join(lines, "\n"), localized
}

type cardManifest struct {
	SchemaVersion string             `json:"schema_version"`
	SourceApp     string             `json:"source_app"`
	Document      cardManifestDoc    `json:"document"`
	Items         []cardManifestItem `json:"items"`
}

type cardManifestDoc struct {
	Title   string   `json:"title"`
	Date    string   `json:"date,omitempty"`
	Period  string   `json:"period"`
	Summary []string `json:"summary"`
}

type cardManifestItem struct {
	ID          string             `json:"id"`
	Category    string             `json:"category"`
	Title       string             `json:"title"`
	Summary     string             `json:"summary"`
	Impact      string             `json:"impact"`
	Source      string             `json:"source,omitempty"`
	PublishedAt string             `json:"published_at,omitempty"`
	URL         string             `json:"url,omitempty"`
	Image       *cardManifestImage `json:"image,omitempty"`
}

type cardManifestImage struct {
	Src string `json:"src"`
	Alt string `json:"alt"`
}

func writeCardManifestSidecar(briefing *model.Briefing, markdownPath string, localizedImages map[string]string) error {
	manifest := buildCardManifest(briefing, localizedImages)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return statefile.WriteAtomicReplaceOnly(cardManifestPath(markdownPath), data, 0o644)
}

func cardManifestPath(markdownPath string) string {
	return strings.TrimSuffix(markdownPath, filepath.Ext(markdownPath)) + ".card-manifest.json"
}

func buildCardManifest(briefing *model.Briefing, localizedImages map[string]string) cardManifest {
	manifest := cardManifest{
		SchemaVersion: "card-article-manifest/v1",
		SourceApp:     "news-briefing",
	}
	if briefing == nil {
		return manifest
	}
	manifest.Document = cardManifestDoc{
		Title:   briefingTitle(briefing.Date, briefing.Period),
		Date:    isoBriefingDate(briefing.Date),
		Period:  briefing.Period,
		Summary: briefingManifestSummary(briefing.StructuredSummary),
	}
	if briefing.StructuredSummary == nil {
		manifest.Items = []cardManifestItem{}
		return manifest
	}
	for _, story := range briefing.StructuredSummary.Stories {
		if strings.TrimSpace(story.Category) != "AI/科技" {
			continue
		}
		item := cardManifestItem{
			ID:       stableManifestItemID(story, briefing.Articles),
			Category: strings.TrimSpace(story.Category),
			Title:    strings.TrimSpace(story.Title),
			Summary:  strings.TrimSpace(story.Summary),
			Impact:   strings.TrimSpace(story.Impact),
		}
		if source := firstSourceArticle(story.SourceArticleIDs, briefing.Articles); source != nil {
			item.Source = strings.TrimSpace(source.Source)
			item.URL = strings.TrimSpace(source.Link)
			if !source.Published.IsZero() {
				item.PublishedAt = source.Published.Format(time.RFC3339)
			}
		}
		if image := cardManifestLocalImage(story.ImageURL, localizedImages); image != "" {
			item.Image = &cardManifestImage{Src: image, Alt: markdownImageAlt(item.Title)}
		}
		manifest.Items = append(manifest.Items, item)
	}
	return manifest
}

func cardManifestLocalImage(raw string, localizedImages map[string]string) string {
	image := html.UnescapeString(strings.TrimSpace(raw))
	if image == "" {
		return ""
	}
	if local := strings.TrimSpace(localizedImages[image]); local != "" {
		return local
	}
	if !isRemoteMarkdownImage(image) {
		return image
	}
	return ""
}

func briefingManifestSummary(summary *model.BriefingSummary) []string {
	if summary == nil {
		return []string{}
	}
	items := []string{}
	for _, group := range summary.OverviewGroups {
		for _, item := range group.Items {
			item = strings.TrimSpace(strings.TrimPrefix(item, "- "))
			if item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

func isoBriefingDate(date string) string {
	if t, err := time.Parse("06.01.02", strings.TrimSpace(date)); err == nil {
		return t.Format("2006-01-02")
	}
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(date)); err == nil {
		return t.Format("2006-01-02")
	}
	return ""
}

func firstSourceArticle(ids []int, articles []model.Article) *model.Article {
	for _, id := range ids {
		idx := id - 1
		if idx >= 0 && idx < len(articles) {
			return &articles[idx]
		}
	}
	return nil
}

func stableManifestItemID(story model.BriefingStory, articles []model.Article) string {
	parts := []string{strings.TrimSpace(story.Category), strings.TrimSpace(story.Title)}
	if source := firstSourceArticle(story.SourceArticleIDs, articles); source != nil {
		parts = append(parts, strings.TrimSpace(source.Link), strings.TrimSpace(source.Source))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
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
