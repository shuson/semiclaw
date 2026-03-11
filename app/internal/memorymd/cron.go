package memorymd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Scheduler struct {
	store  *Store
	onRun  func(context.Context, AutomationJob) error
	ticker time.Duration
}

func NewScheduler(store *Store, ticker time.Duration, onRun func(context.Context, AutomationJob) error) *Scheduler {
	if ticker <= 0 {
		ticker = 30 * time.Second
	}
	return &Scheduler{
		store:  store,
		onRun:  onRun,
		ticker: ticker,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	t := time.NewTicker(s.ticker)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) error {
	scopes, err := s.store.ListAutomationScopes()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, agentName := range scopes {
		jobs, listErr := s.store.ListAutomations(agentName)
		if listErr != nil {
			continue
		}
		for _, job := range jobs {
			if !job.Enabled || job.NextRunAt.IsZero() || job.NextRunAt.After(now) {
				continue
			}

			runErr := error(nil)
			if s.onRun != nil {
				runErr = s.onRun(ctx, job)
			}
			status := "success"
			note := "executed"
			if runErr != nil {
				status = "error"
				note = runErr.Error()
			}

			job.LastRunAt = now
			next, nextErr := NextRun(job.CronExpr, job.TZ, now.Add(time.Minute))
			if nextErr != nil {
				job.Enabled = false
				note = "disabled due to invalid cron: " + nextErr.Error()
				status = "error"
			} else {
				job.NextRunAt = next
			}

			_ = s.store.AppendAutomationRun(agentName, job, status, note)
			_ = s.store.SaveAutomationState(agentName, job)
		}
	}
	return nil
}

func NextRun(expr string, tz string, from time.Time) (time.Time, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have 5 fields")
	}

	loc := time.UTC
	if strings.TrimSpace(tz) != "" {
		loaded, err := time.LoadLocation(strings.TrimSpace(tz))
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid timezone %q", tz)
		}
		loc = loaded
	}

	start := from.In(loc).Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 525600; i++ {
		t := start.Add(time.Duration(i) * time.Minute)
		if matchCronField(fields[0], t.Minute(), 0, 59) &&
			matchCronField(fields[1], t.Hour(), 0, 23) &&
			matchCronField(fields[2], t.Day(), 1, 31) &&
			matchCronField(fields[3], int(t.Month()), 1, 12) &&
			matchCronField(fields[4], int(t.Weekday()), 0, 6) {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("could not compute next run within one year")
}

func matchCronField(field string, value int, min int, max int) bool {
	field = strings.TrimSpace(field)
	if field == "*" {
		return true
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		if n < min || n > max {
			return false
		}
		if n == value {
			return true
		}
	}
	return false
}
