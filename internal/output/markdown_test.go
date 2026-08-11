package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestWriteMarkdownPreservesBodyOrderAndSingleTitle(t *testing.T) {
	outputDir := t.TempDir()
	path, err := WriteMarkdown(&model.Briefing{
		Date:       "26.03.27",
		Period:     "1400",
		RawContent: "TRANSLATED\n\n---\n\nORIGINAL",
	}, outputDir)
	if err != nil {
		t.Fatalf("WriteMarkdown() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	title := "# 国际资讯简报 26.03.27 午间 14:00"
	if strings.Count(got, title) != 1 {
		t.Fatalf("WriteMarkdown() title count = %d, want 1 in %q", strings.Count(got, title), got)
	}
	if !strings.Contains(got, "TRANSLATED\n\n---\n\nORIGINAL") {
		t.Fatalf("WriteMarkdown() body = %q", got)
	}
}

func TestWriteMarkdownKeepsDefaultHeaderAndOmitsTopHeroImage(t *testing.T) {
	outputDir := t.TempDir()
	path, err := WriteMarkdown(&model.Briefing{
		Date:       "26.06.06",
		Period:     "0800",
		Articles:   []model.Article{{ImageURL: "https://example.com/hero.jpg"}},
		RawContent: "## 今日速览\n\n- Claude 服务异常\n\n## AI/科技\n\n### Claude 服务异常\n**摘要：** ...",
	}, outputDir)
	if err != nil {
		t.Fatalf("WriteMarkdown() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	wantPrefix := "# 国际资讯简报 26.06.06 早间 08:00\n\n## 今日速览"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("WriteMarkdown() = %q, want prefix %q", got, wantPrefix)
	}
	if strings.Contains(got, "![封面图]") {
		t.Fatalf("WriteMarkdown() injected top hero image: %q", got)
	}
}

func TestWriteMarkdownWritesCardManifestFromStructuredBriefing(t *testing.T) {
	outputDir := t.TempDir()
	imageURL := "https://cdn.example.com/openai.jpg"
	download := func(rawURL string, path string) error {
		if rawURL != imageURL {
			t.Fatalf("download URL = %q, want %q", rawURL, imageURL)
		}
		return os.WriteFile(path, []byte("image bytes"), 0o644)
	}
	briefing := &model.Briefing{
		Date:   "26.06.16",
		Period: "1800",
		Articles: []model.Article{
			{
				Title:     "OpenAI 发布新功能",
				Summary:   "原始摘要",
				ImageURL:  imageURL,
				Source:    "The Verge",
				Category:  "AI/科技",
				Link:      "https://example.com/openai",
				Published: time.Date(2026, 6, 16, 11, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
			},
			{
				Title:     "外交新闻",
				Source:    "BBC",
				Category:  "国际政治",
				Link:      "https://example.com/world",
				Published: time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC),
			},
		},
		StructuredSummary: &model.BriefingSummary{
			OverviewGroups: []model.BriefingOverviewGroup{{Category: "AI/科技", Items: []string{"要点一", "要点二"}}},
			XHSTopics:      []string{"#AI新闻", "科技资讯", "AI新闻", "财经观察", "国际新闻"},
			Stories: []model.BriefingStory{
				{Category: "AI/科技", Title: "OpenAI 发布新功能", Summary: "摘要正文", Impact: "影响正文", ImageURL: imageURL, SourceArticleIDs: []int{1}},
				{Category: "国际政治", Title: "外交新闻", Summary: "不应进入 manifest", SourceArticleIDs: []int{2}},
			},
		},
		RawContent: strings.Join([]string{
			"## 今日速览",
			"",
			"### AI/科技",
			"",
			"- 要点一",
			"- 要点二",
			"",
			"## AI/科技",
			"",
			"### OpenAI 发布新功能",
			"![OpenAI 发布新功能](" + imageURL + ")",
			"**摘要：** 摘要正文  ",
			"**影响：** 影响正文  ",
		}, "\n"),
	}
	path, err := writeMarkdownWithImageDownloader(briefing, outputDir, download)
	if err != nil {
		t.Fatalf("writeMarkdownWithImageDownloader() error = %v", err)
	}

	manifestPath := strings.TrimSuffix(path, ".md") + ".card-manifest.json"
	data, err := os.ReadFile(filepath.Clean(manifestPath))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	var got struct {
		SchemaVersion string `json:"schema_version"`
		SourceApp     string `json:"source_app"`
		Document      struct {
			Title     string   `json:"title"`
			Date      string   `json:"date"`
			Period    string   `json:"period"`
			Summary   []string `json:"summary"`
			XHSTopics []string `json:"xhs_topics"`
		} `json:"document"`
		Items []struct {
			ID          string `json:"id"`
			Category    string `json:"category"`
			Title       string `json:"title"`
			Summary     string `json:"summary"`
			Impact      string `json:"impact"`
			Source      string `json:"source"`
			PublishedAt string `json:"published_at"`
			URL         string `json:"url"`
			Image       *struct {
				Src string `json:"src"`
				Alt string `json:"alt"`
			} `json:"image"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(manifest) error = %v", err)
	}
	if got.SchemaVersion != "card-article-manifest/v1" || got.SourceApp != "news-briefing" {
		t.Fatalf("manifest identity = %#v", got)
	}
	if got.Document.Title != "国际资讯简报 26.06.16 晚间 18:00" || got.Document.Date != "2026-06-16" || got.Document.Period != "1800" {
		t.Fatalf("manifest document = %#v", got.Document)
	}
	if want := []string{"AI新闻", "科技资讯", "财经观察", "国际新闻"}; !reflect.DeepEqual(got.Document.XHSTopics, want) {
		t.Fatalf("manifest xhs_topics = %#v, want %#v", got.Document.XHSTopics, want)
	}
	if strings.Join(got.Document.Summary, ",") != "要点一,要点二" {
		t.Fatalf("manifest summary = %#v", got.Document.Summary)
	}
	if len(got.Items) != 2 {
		t.Fatalf("len(manifest.items) = %d, want 2: %#v", len(got.Items), got.Items)
	}
	item := got.Items[0]
	if item.ID == "" || item.Category != "AI/科技" || item.Title != "OpenAI 发布新功能" || item.Summary != "摘要正文" || item.Impact != "影响正文" || item.Source != "The Verge" || item.PublishedAt != "2026-06-16T11:00:00+08:00" || item.URL != "https://example.com/openai" {
		t.Fatalf("manifest item = %#v", item)
	}
	if item.Image == nil || !strings.HasPrefix(item.Image.Src, "assets/26.06.16-1800/") || item.Image.Alt != "OpenAI 发布新功能" {
		t.Fatalf("manifest image = %#v", item.Image)
	}
	if got.Items[1].Category != "国际政治" || got.Items[1].Title != "外交新闻" {
		t.Fatalf("second manifest item = %#v", got.Items[1])
	}
}

func TestWriteMarkdownManifestOmitsMissingImage(t *testing.T) {
	outputDir := t.TempDir()
	path, err := WriteMarkdown(&model.Briefing{
		Date:   "26.06.16",
		Period: "0800",
		Articles: []model.Article{{
			Title:     "OpenAI 发布新功能",
			Source:    "OpenAI News",
			Category:  "AI/科技",
			Link:      "https://example.com/openai",
			Published: time.Date(2026, 6, 16, 1, 0, 0, 0, time.UTC),
		}},
		StructuredSummary: &model.BriefingSummary{
			OverviewGroups: []model.BriefingOverviewGroup{{Category: "AI/科技", Items: []string{"要点一"}}},
			Stories:        []model.BriefingStory{{Category: "AI/科技", Title: "OpenAI 发布新功能", Summary: "摘要正文", Impact: "影响正文", SourceArticleIDs: []int{1}}},
		},
		RawContent: "## 今日速览\n\n- 要点一\n\n## AI/科技\n\n### OpenAI 发布新功能\n**摘要：** 摘要正文",
	}, outputDir)
	if err != nil {
		t.Fatalf("WriteMarkdown() error = %v", err)
	}

	manifestPath := strings.TrimSuffix(path, ".md") + ".card-manifest.json"
	data, err := os.ReadFile(filepath.Clean(manifestPath))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if strings.Contains(string(data), "\"image\"") {
		t.Fatalf("manifest should omit missing image: %s", string(data))
	}
}

func TestWriteMarkdownManifestOmitsImageWhenDownloadFails(t *testing.T) {
	outputDir := t.TempDir()
	imageURL := "https://example.com/openai.jpg"
	path, err := writeMarkdownWithImageDownloader(&model.Briefing{
		Date:   "26.06.16",
		Period: "0800",
		Articles: []model.Article{{
			Title:    "OpenAI 发布新功能",
			Source:   "OpenAI News",
			Category: "AI/科技",
			Link:     "https://example.com/openai",
		}},
		StructuredSummary: &model.BriefingSummary{
			OverviewGroups: []model.BriefingOverviewGroup{{Category: "AI/科技", Items: []string{"要点一"}}},
			Stories:        []model.BriefingStory{{Category: "AI/科技", Title: "OpenAI 发布新功能", Summary: "摘要正文", Impact: "影响正文", ImageURL: imageURL, SourceArticleIDs: []int{1}}},
		},
		RawContent: "## 今日速览\n\n- 要点一\n\n## AI/科技\n\n### OpenAI 发布新功能\n![OpenAI 发布新功能](" + imageURL + ")\n**摘要：** 摘要正文",
	}, outputDir, func(rawURL string, path string) error {
		return fmt.Errorf("download failed")
	})
	if err != nil {
		t.Fatalf("writeMarkdownWithImageDownloader() error = %v", err)
	}

	manifestPath := strings.TrimSuffix(path, ".md") + ".card-manifest.json"
	data, err := os.ReadFile(filepath.Clean(manifestPath))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if strings.Contains(string(data), "\"image\"") || strings.Contains(string(data), imageURL) {
		t.Fatalf("manifest should omit failed remote image: %s", string(data))
	}
}

func TestWriteMarkdownManifestOmitsCardUnusableImage(t *testing.T) {
	outputDir := t.TempDir()
	imageURL := "https://cdn.example.com/openai.png"
	path, err := writeMarkdownWithImageDownloader(&model.Briefing{
		Date:   "26.06.16",
		Period: "0800",
		Articles: []model.Article{{
			Title:    "OpenAI 发布新功能",
			Source:   "OpenAI News",
			Category: "AI/科技",
			Link:     "https://example.com/openai",
		}},
		StructuredSummary: &model.BriefingSummary{
			OverviewGroups: []model.BriefingOverviewGroup{{Category: "AI/科技", Items: []string{"要点一"}}},
			Stories:        []model.BriefingStory{{Category: "AI/科技", Title: "OpenAI 发布新功能", Summary: "摘要正文", Impact: "影响正文", ImageURL: imageURL, SourceArticleIDs: []int{1}}},
		},
		RawContent: "## 今日速览\n\n- 要点一\n\n## AI/科技\n\n### OpenAI 发布新功能\n![OpenAI 发布新功能](" + imageURL + ")\n**摘要：** 摘要正文",
	}, outputDir, func(rawURL string, path string) error {
		if rawURL != imageURL {
			t.Fatalf("download URL = %q, want %q", rawURL, imageURL)
		}
		return writeValidatedMarkdownImage(path, testPNGBytes(t, 140, 140))
	})
	if err != nil {
		t.Fatalf("writeMarkdownWithImageDownloader() error = %v", err)
	}

	markdownData, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile(markdown) error = %v", err)
	}
	markdown := string(markdownData)
	if strings.Contains(markdown, imageURL) || strings.Contains(markdown, "![OpenAI 发布新功能]") {
		t.Fatalf("markdown kept card-unusable image: %s", markdown)
	}

	manifestPath := strings.TrimSuffix(path, ".md") + ".card-manifest.json"
	manifestData, err := os.ReadFile(filepath.Clean(manifestPath))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	manifest := string(manifestData)
	if strings.Contains(manifest, "\"image\"") || strings.Contains(manifest, imageURL) {
		t.Fatalf("manifest should omit card-unusable image: %s", manifest)
	}
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	oldStderr := os.Stderr
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	os.Stderr = writePipe
	defer func() { os.Stderr = oldStderr }()

	run()

	if err := writePipe.Close(); err != nil {
		t.Fatalf("Close(writePipe) error = %v", err)
	}
	data, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("ReadAll(stderr) error = %v", err)
	}
	if err := readPipe.Close(); err != nil {
		t.Fatalf("Close(readPipe) error = %v", err)
	}
	return string(data)
}

func testPNGBytes(t *testing.T, width int, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 40, G: 80, B: 120, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return out.Bytes()
}

func testJPEGBytes(t *testing.T, width int, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8((x + y) % 256), G: uint8((x * 3) % 256), B: uint8((y * 5) % 256), A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode() error = %v", err)
	}
	return out.Bytes()
}

func paddedImageBytes(data []byte, size int) []byte {
	if len(data) >= size {
		return data
	}
	out := make([]byte, size)
	copy(out, data)
	return out
}

func TestWriteMarkdownDownloadsRemoteStoryImagesToAssets(t *testing.T) {
	outputDir := t.TempDir()
	var downloadedURL string
	var downloadedPath string
	download := func(rawURL string, path string) error {
		downloadedURL = rawURL
		downloadedPath = path
		return os.WriteFile(path, []byte("image bytes"), 0o644)
	}
	briefing := &model.Briefing{
		Date:   "26.06.06",
		Period: "0800",
		RawContent: strings.Join([]string{
			"## AI/科技",
			"",
			"### Claude 服务异常",
			"![Claude 服务异常](https://cdn.example.com/images/claude.jpg?width=1200)",
			"**摘要：** ...",
		}, "\n"),
	}
	path, err := writeMarkdownWithImageDownloader(briefing, outputDir, download)
	if err != nil {
		t.Fatalf("writeMarkdownWithImageDownloader() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if strings.Contains(got, "https://cdn.example.com/images/claude.jpg") {
		t.Fatalf("WriteMarkdown() kept remote image URL: %q", got)
	}
	if !strings.Contains(briefing.RawContent, "https://cdn.example.com/images/claude.jpg?width=1200") {
		t.Fatalf("WriteMarkdown() mutated briefing raw content: %q", briefing.RawContent)
	}
	wantImagePrefix := "![Claude 服务异常](assets/26.06.06-0800/"
	if !strings.Contains(got, wantImagePrefix) || !strings.Contains(got, ".jpg)") {
		t.Fatalf("WriteMarkdown() image path = %q, want local jpg under %q", got, wantImagePrefix)
	}
	if downloadedURL != "https://cdn.example.com/images/claude.jpg?width=1200" {
		t.Fatalf("downloaded URL = %q", downloadedURL)
	}
	if !strings.HasPrefix(downloadedPath, filepath.Join(outputDir, "assets", "26.06.06-0800")) {
		t.Fatalf("downloaded path = %q", downloadedPath)
	}
	assetData, err := os.ReadFile(downloadedPath)
	if err != nil {
		t.Fatalf("ReadFile(asset) error = %v", err)
	}
	if string(assetData) != "image bytes" {
		t.Fatalf("asset data = %q", string(assetData))
	}
}

func TestWriteMarkdownRemovesTrackingPixelImage(t *testing.T) {
	outputDir := t.TempDir()
	downloadCalled := false
	download := func(rawURL string, path string) error {
		downloadCalled = true
		return nil
	}
	imageURL := "https://media.npr.org/include/images/tracking/npr-rss-pixel.png?story=nx-s1"
	briefing := &model.Briefing{
		Date:   "26.06.06",
		Period: "1800",
		RawContent: strings.Join([]string{
			"## AI/科技",
			"",
			"### Claude 旧模型退役",
			"![Claude 旧模型退役](" + imageURL + ")",
			"**摘要：** ...",
		}, "\n"),
	}
	path, err := writeMarkdownWithImageDownloader(briefing, outputDir, download)
	if err != nil {
		t.Fatalf("writeMarkdownWithImageDownloader() error = %v", err)
	}
	if downloadCalled {
		t.Fatal("download called for tracking pixel")
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if strings.Contains(got, imageURL) || strings.Contains(got, "![Claude 旧模型退役]") {
		t.Fatalf("markdown kept tracking image: %q", got)
	}
}

func TestWriteMarkdownRemovesRemoteStoryImageWhenDownloadFails(t *testing.T) {
	outputDir := t.TempDir()
	imageURL := "https://cdn.example.com/images/story.jpg"
	download := func(rawURL string, path string) error {
		if rawURL != imageURL {
			t.Fatalf("download URL = %q, want %q", rawURL, imageURL)
		}
		return fmt.Errorf("download image: 403 Forbidden")
	}
	briefing := &model.Briefing{
		Date:   "26.06.06",
		Period: "1800",
		RawContent: strings.Join([]string{
			"## 国际政治",
			"",
			"### 美伊海湾交火",
			"![美伊海湾交火](" + imageURL + ")",
			"**摘要：** ...",
		}, "\n"),
	}
	var path string
	stderr := captureStderr(t, func() {
		var err error
		path, err = writeMarkdownWithImageDownloader(briefing, outputDir, download)
		if err != nil {
			t.Fatalf("writeMarkdownWithImageDownloader() error = %v", err)
		}
	})

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if strings.Contains(got, imageURL) || strings.Contains(got, "![美伊海湾交火]") {
		t.Fatalf("markdown kept failed remote image: %q", got)
	}
	for _, want := range []string{"### 美伊海湾交火", "**摘要：** ..."} {
		if !strings.Contains(got, want) {
			t.Fatalf("markdown removed story content: %q", got)
		}
	}
	for _, want := range []string{"Markdown image download failed", "cdn.example.com", "403 Forbidden"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want contain %q", stderr, want)
		}
	}
}

func TestDownloadMarkdownImageAcceptsLargeImageAndCompressesOutput(t *testing.T) {
	imageData := paddedImageBytes(testJPEGBytes(t, 640, 373), 12*1024*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.jpg")
	if err := downloadMarkdownImage(server.URL+"/image.jpg", path); err != nil {
		t.Fatalf("downloadMarkdownImage() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(got) > 5*1024*1024 {
		t.Fatalf("downloaded image size = %d, want at most 5 MiB", len(got))
	}
	if len(got) >= len(imageData) {
		t.Fatalf("downloaded image size = %d, want compressed below original %d", len(got), len(imageData))
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	if _, err := jpeg.DecodeConfig(file); err != nil {
		t.Fatalf("jpeg.DecodeConfig() error = %v", err)
	}
}

func TestDownloadMarkdownImageRejectsImageOverThirtyMegabytes(t *testing.T) {
	imageData := paddedImageBytes(testJPEGBytes(t, 640, 373), 31*1024*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.jpg")
	err := downloadMarkdownImage(server.URL+"/image.jpg", path)
	if err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("downloadMarkdownImage() error = %v, want file too large", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("downloaded oversized image exists, stat error = %v", statErr)
	}
}

func TestDownloadMarkdownImageAcceptsOctetStreamForImageURL(t *testing.T) {
	imageData := testPNGBytes(t, 640, 373)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "image/webp") {
			t.Fatalf("Accept header = %q, want no image/webp preference", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.png")
	if err := downloadMarkdownImage(server.URL+"/image.png", path); err != nil {
		t.Fatalf("downloadMarkdownImage() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, imageData) {
		t.Fatalf("downloaded data changed for matching PNG")
	}
}

func TestDownloadMarkdownImageRejectsTinyImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNGBytes(t, 1, 1))
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.png")
	err := downloadMarkdownImage(server.URL+"/image.png", path)
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("downloadMarkdownImage() error = %v, want too small", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("downloaded tiny image exists, stat error = %v", statErr)
	}
}

func TestDownloadMarkdownImageRejectsCardUnusableImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNGBytes(t, 140, 140))
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.png")
	err := downloadMarkdownImage(server.URL+"/image.png", path)
	if err == nil || !strings.Contains(err.Error(), "too small for card") {
		t.Fatalf("downloadMarkdownImage() error = %v, want too small for card", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("downloaded card-unusable image exists, stat error = %v", statErr)
	}
}

func TestDownloadMarkdownImageAcceptsMediumCardImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNGBytes(t, 640, 373))
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.png")
	if err := downloadMarkdownImage(server.URL+"/image.png", path); err != nil {
		t.Fatalf("downloadMarkdownImage() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("downloaded medium card image missing: %v", err)
	}
}

func TestDownloadMarkdownImageConvertsMismatchedImageFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(testPNGBytes(t, 640, 373))
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.jpg")
	if err := downloadMarkdownImage(server.URL+"/image.jpg", path); err != nil {
		t.Fatalf("downloadMarkdownImage() error = %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	config, err := jpeg.DecodeConfig(file)
	if err != nil {
		t.Fatalf("jpeg.DecodeConfig() error = %v", err)
	}
	if config.Width != 640 || config.Height != 373 {
		t.Fatalf("decoded JPEG size = %dx%d, want 640x373", config.Width, config.Height)
	}
}

func TestDownloadMarkdownImageFallsBackToEmbeddedImageURL(t *testing.T) {
	imageData := testPNGBytes(t, 640, 373)
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/resize/6152x4100!/":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/original/ap26156702656210.jpg":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	originalURL := server.URL + "/original/ap26156702656210.jpg"
	resizeURL := server.URL + "/resize/6152x4100!/?url=" + url.QueryEscape(originalURL)
	path := filepath.Join(t.TempDir(), "image.jpg")
	if err := downloadMarkdownImage(resizeURL, path); err != nil {
		t.Fatalf("downloadMarkdownImage() error = %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	if _, err := jpeg.DecodeConfig(file); err != nil {
		t.Fatalf("jpeg.DecodeConfig() error = %v", err)
	}
	wantRequests := []string{"/resize/6152x4100!/", "/resize/6152x4100!/", "/resize/6152x4100!/", "/original/ap26156702656210.jpg"}
	if strings.Join(requested, ",") != strings.Join(wantRequests, ",") {
		t.Fatalf("requested paths = %#v, want %#v", requested, wantRequests)
	}
}

func TestDownloadMarkdownImageRetriesTemporaryFailures(t *testing.T) {
	imageData := testPNGBytes(t, 640, 373)
	attemptTimes := make([]time.Time, 0, markdownImageDownloadAttempts)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptTimes = append(attemptTimes, time.Now())
		if len(attemptTimes) < 3 {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageData)
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.png")
	if err := downloadMarkdownImage(server.URL+"/image.png", path); err != nil {
		t.Fatalf("downloadMarkdownImage() error = %v", err)
	}
	if len(attemptTimes) != 3 {
		t.Fatalf("attempts = %d, want 3", len(attemptTimes))
	}
	for i := 1; i < len(attemptTimes); i++ {
		if gap := attemptTimes[i].Sub(attemptTimes[i-1]); gap < 180*time.Millisecond {
			t.Fatalf("retry gap %d = %v, want at least 180ms", i, gap)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, imageData) {
		t.Fatalf("downloaded data changed for matching PNG")
	}
}

func TestWriteMarkdownUnescapesRemoteImageURLBeforeDownload(t *testing.T) {
	outputDir := t.TempDir()
	var downloadedURL string
	download := func(rawURL string, path string) error {
		downloadedURL = rawURL
		return os.WriteFile(path, []byte("image bytes"), 0o644)
	}
	briefing := &model.Briefing{
		Date:   "26.06.06",
		Period: "0800",
		RawContent: strings.Join([]string{
			"## AI/科技",
			"",
			"### Reddit 预览图",
			"![Reddit 预览图](https://preview.redd.it/story.png?width=640&amp;crop=smart&amp;auto=webp)",
			"**摘要：** ...",
		}, "\n"),
	}

	if _, err := writeMarkdownWithImageDownloader(briefing, outputDir, download); err != nil {
		t.Fatalf("writeMarkdownWithImageDownloader() error = %v", err)
	}
	want := "https://preview.redd.it/story.png?width=640&crop=smart&auto=webp"
	if downloadedURL != want {
		t.Fatalf("downloaded URL = %q, want %q", downloadedURL, want)
	}
}

func TestWriteDeepDiveUsesTopicDeepDivePackHeader(t *testing.T) {
	outputDir := t.TempDir()
	path, err := WriteDeepDive("OpenAI", "正文", outputDir, "26.03.26")
	if err != nil {
		t.Fatalf("WriteDeepDive() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "# 话题深挖包：OpenAI") {
		t.Fatalf("WriteDeepDive() header = %q", got)
	}
	if strings.Contains(got, "# 深度素材包：OpenAI") {
		t.Fatalf("WriteDeepDive() kept legacy header: %q", got)
	}
}
