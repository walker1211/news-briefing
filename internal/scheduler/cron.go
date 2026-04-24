package scheduler

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/logutil"
)

type Window struct {
	Expr   string
	Period string
	From   time.Time
	To     time.Time
}

// Start 注册所有定时任务并启动调度器。
func Start(cfg *config.Config, runFunc func(Window)) error {
	if cfg == nil {
		return fmt.Errorf("scheduler config 不能为空")
	}
	if len(cfg.Schedule) == 0 {
		return fmt.Errorf("schedule 不能为空")
	}

	loc := cfg.ScheduleLocation
	if loc == nil {
		loc = time.Local
	}
	c := cron.New(cron.WithLocation(loc))

	for _, expr := range cfg.Schedule {
		expr := expr
		var entryID cron.EntryID
		var err error
		entryID, err = c.AddFunc(expr, func() {
			entry := c.Entry(entryID)
			if entry.Prev.IsZero() {
				logutil.Printf("[scheduler] 跳过定时任务 %s：上次触发时间为空", expr)
				return
			}
			window, buildErr := buildWindow(entry.Prev, expr, cfg.Schedule, loc)
			if buildErr != nil {
				logutil.Printf("[scheduler] 跳过定时任务 %s：%v", expr, buildErr)
				return
			}
			logutil.Printf("[scheduler] 触发定时任务: %s [%s -> %s] ...", expr, window.From.In(loc).Format(time.RFC3339), window.To.In(loc).Format(time.RFC3339))
			runFunc(window)
		})
		if err != nil {
			return fmt.Errorf("添加定时任务 %q 失败: %w", expr, err)
		}
	}

	c.Start()
	logutil.Printf("[scheduler] 已启动，共 %d 个时间点 (%s)", len(cfg.Schedule), loc.String())
	for _, expr := range cfg.Schedule {
		logutil.Printf("  - %s", expr)
	}

	return nil
}

func buildWindow(to time.Time, expr string, schedules []string, loc *time.Location) (Window, error) {
	if loc == nil {
		loc = time.Local
	}
	if to.IsZero() {
		return Window{}, fmt.Errorf("window 结束时间不能为空")
	}
	if len(schedules) == 0 {
		return Window{}, fmt.Errorf("schedule 不能为空")
	}

	to = to.In(loc)
	from, err := latestScheduledPointBefore(to, schedules, loc)
	if err != nil {
		return Window{}, err
	}
	if !from.Before(to) {
		return Window{}, fmt.Errorf("window 起点 %s 必须早于终点 %s", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}

	return Window{
		Expr:   expr,
		Period: to.Format("1504"),
		From:   from,
		To:     to,
	}, nil
}

func latestScheduledPointBefore(to time.Time, schedules []string, loc *time.Location) (time.Time, error) {
	var latest time.Time
	for _, expr := range schedules {
		schedule, err := cron.ParseStandard(expr)
		if err != nil {
			return time.Time{}, fmt.Errorf("解析定时任务 %q 失败: %w", expr, err)
		}
		candidate, err := previousScheduledPoint(schedule, to, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("推导定时任务 %q 的窗口起点失败: %w", expr, err)
		}
		if latest.IsZero() || candidate.After(latest) {
			latest = candidate
		}
	}
	if latest.IsZero() {
		return time.Time{}, fmt.Errorf("未找到 %s 之前的调度时间点", to.Format(time.RFC3339))
	}
	return latest, nil
}

func previousScheduledPoint(schedule cron.Schedule, to time.Time, loc *time.Location) (time.Time, error) {
	searchWindows := []time.Duration{
		2 * time.Minute,
		2 * time.Hour,
		48 * time.Hour,
		31 * 24 * time.Hour,
		366 * 24 * time.Hour,
		5 * 366 * 24 * time.Hour,
	}
	for _, window := range searchWindows {
		start := to.Add(-window)
		cursor := start
		var previous time.Time
		for i := 0; i < 100000; i++ {
			next := schedule.Next(cursor)
			if next.IsZero() {
				return time.Time{}, fmt.Errorf("cron 计算返回零值: cursor=%s", cursor.Format(time.RFC3339))
			}
			if !next.After(cursor) {
				return time.Time{}, fmt.Errorf("cron 计算未前进: cursor=%s next=%s", cursor.Format(time.RFC3339), next.Format(time.RFC3339))
			}
			if !next.Before(to) {
				if !previous.IsZero() {
					return previous.In(loc), nil
				}
				break
			}
			previous = next
			cursor = next
		}
	}
	return time.Time{}, fmt.Errorf("未找到 %s 之前的上一个触发时间", to.Format(time.RFC3339))
}
