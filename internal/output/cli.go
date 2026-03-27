package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/walker1211/news-briefing/internal/model"
)

// isTTY 检测是否为终端输出
var isTTY bool

func init() {
	fi, _ := os.Stdout.Stat()
	isTTY = (fi.Mode() & os.ModeCharDevice) != 0
}

func PrintCLI(briefing *model.Briefing) {
	writeCLI(os.Stdout, briefing, isTTY)
}

func writeCLI(w io.Writer, briefing *model.Briefing, useColor bool) {
	color := func(code string) string {
		if useColor {
			return code
		}
		return ""
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s%s==============================%s\n", color("\033[1m"), color("\033[36m"), color("\033[0m"))
	fmt.Fprintf(w, "%s%s %s %s\n", color("\033[1m"), color("\033[36m"), briefingTitle(briefing.Date, briefing.Period), color("\033[0m"))
	fmt.Fprintf(w, "%s%s==============================%s\n", color("\033[1m"), color("\033[36m"), color("\033[0m"))
	fmt.Fprintln(w)

	for line := range strings.SplitSeq(briefing.RawContent, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			fmt.Fprintf(w, "%s%s%s%s\n", color("\033[1m"), color("\033[32m"), line, color("\033[0m"))
		case strings.HasPrefix(line, "### "):
			fmt.Fprintf(w, "%s%s%s%s\n", color("\033[1m"), color("\033[33m"), line, color("\033[0m"))
		case strings.HasPrefix(line, "> "):
			fmt.Fprintf(w, "%s%s%s\n", color("\033[31m"), line, color("\033[0m"))
		case strings.HasPrefix(line, "**"):
			fmt.Fprintf(w, "%s%s%s\n", color("\033[1m"), line, color("\033[0m"))
		default:
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w)
}

// periodPrefix 将 "HHMM" 格式的时间标识转为中文时段前缀，如 "0800" → "早间"
func periodPrefix(p string) string {
	if len(p) != 4 {
		return p
	}
	hh := p[:2]
	switch {
	case hh < "06":
		return "凌晨"
	case hh < "12":
		return "早间"
	case hh < "18":
		return "午间"
	default:
		return "晚间"
	}
}
