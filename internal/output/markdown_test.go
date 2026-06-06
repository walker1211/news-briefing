package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
