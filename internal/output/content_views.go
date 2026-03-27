package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

var cst = time.FixedZone("CST", 8*3600)

func ArticleListView(articles []model.Article) string {
	var sb strings.Builder
	for i, a := range articles {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n   %s\n   Source: %s | %s\n   Link: %s\n\n",
			i+1, a.Category, a.Title, a.Summary, a.Source,
			a.Published.In(cst).Format("2006-01-02 15:04"), a.Link))
	}
	return sb.String()
}

func GroupedArticleListView(articles []model.Article) string {
	grouped := make(map[string][]model.Article)
	for _, a := range articles {
		grouped[a.Category] = append(grouped[a.Category], a)
	}

	var sb strings.Builder
	n := 1
	for _, cat := range []string{"AI/科技", "国际政治"} {
		items := grouped[cat]
		if len(items) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("== %s (%d篇) ==\n\n", cat, len(items)))
		for _, a := range items {
			sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   Source: %s | %s\n   Link: %s\n\n",
				n, a.Title, a.Summary, a.Source,
				a.Published.In(cst).Format("2006-01-02 15:04"), a.Link))
			n++
		}
	}
	return sb.String()
}
