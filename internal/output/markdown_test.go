package output

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func TestWriteMarkdownWarnsWhenRemoteStoryImageDownloadFails(t *testing.T) {
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
	if !strings.Contains(string(data), imageURL) {
		t.Fatalf("markdown = %q, want original URL retained", string(data))
	}
	for _, want := range []string{"Markdown image download failed", "cdn.example.com", "403 Forbidden"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want contain %q", stderr, want)
		}
	}
}

func TestDownloadMarkdownImageAcceptsOctetStreamForImageURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "image/webp") {
			t.Fatalf("Accept header = %q, want image/webp", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("webp bytes"))
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "image.webp")
	if err := downloadMarkdownImage(server.URL+"/image.webp", path); err != nil {
		t.Fatalf("downloadMarkdownImage() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "webp bytes" {
		t.Fatalf("downloaded data = %q", string(got))
	}
}

func TestDownloadMarkdownImageFallsBackToEmbeddedImageURL(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/resize/6152x4100!/":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/original/ap26156702656210.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg bytes"))
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
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "jpeg bytes" {
		t.Fatalf("downloaded data = %q", string(got))
	}
	wantRequests := []string{"/resize/6152x4100!/", "/resize/6152x4100!/", "/resize/6152x4100!/", "/original/ap26156702656210.jpg"}
	if strings.Join(requested, ",") != strings.Join(wantRequests, ",") {
		t.Fatalf("requested paths = %#v, want %#v", requested, wantRequests)
	}
}

func TestDownloadMarkdownImageRetriesTemporaryFailures(t *testing.T) {
	attemptTimes := make([]time.Time, 0, markdownImageDownloadAttempts)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptTimes = append(attemptTimes, time.Now())
		if len(attemptTimes) < 3 {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png bytes"))
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
	if string(got) != "png bytes" {
		t.Fatalf("downloaded data = %q", string(got))
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
