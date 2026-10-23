package limits

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var (
	future     = time.Now().Add(2 * time.Hour).Truncate(time.Second)
	farFuture  = time.Now().Add(72 * time.Hour).Truncate(time.Second)
	cachedAt   = time.Now().Add(-4 * time.Hour).Truncate(time.Millisecond)
	statuslnAt = time.Now().Add(-2 * time.Minute).Truncate(time.Second)
)

func writeAt(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func claudeJSON(fetched time.Time, five, seven int, resets time.Time) string {
	return fmt.Sprintf(`{"other":1,"cachedUsageUtilization":{"fetchedAtMs":%d,"utilization":{"five_hour":{"utilization":%d,"resets_at":%q},"seven_day":{"utilization":%d,"resets_at":%q}}}}`,
		fetched.UnixMilli(), five, resets.Format(time.RFC3339Nano), seven, farFuture.Format(time.RFC3339Nano))
}

func statuslineJSON(updated time.Time, five int, resets time.Time) string {
	return fmt.Sprintf(`{"source":"claude","updated_at":%d,"five_hour":{"used_percentage":%d,"resets_at":%d},"seven_day":{"used_percentage":21,"resets_at":%d}}`,
		updated.Unix(), five, resets.Unix(), farFuture.Unix())
}

func claudePath(home string) string { return filepath.Join(home, ".claude.json") }
func statusPath(home string) string { return filepath.Join(home, ".claude", "abtop-rate-limits.json") }
func now() time.Time                { return time.Now() }

func TestCachedUsageAlone(t *testing.T) {
	home := t.TempDir()
	writeAt(t, claudePath(home), claudeJSON(cachedAt, 3, 12, future), now())
	got, ok := Load(home)
	if !ok || got.FiveHour.Percent != 3 || got.SevenDay.Percent != 12 || !got.FiveHour.Known || !got.AsOf.Equal(cachedAt) || !got.FiveHour.ResetsAt.Equal(future) {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestStatuslineFileAlone(t *testing.T) {
	home := t.TempDir()
	writeAt(t, statusPath(home), statuslineJSON(statuslnAt, 86, future), now())
	got, ok := Load(home)
	if !ok || got.FiveHour.Percent != 86 || got.SevenDay.Percent != 21 || !got.AsOf.Equal(statuslnAt) || !got.FiveHour.ResetsAt.Equal(future) {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestFresherSourceWinsEitherWay(t *testing.T) {
	home := t.TempDir()
	writeAt(t, claudePath(home), claudeJSON(cachedAt, 3, 12, future), now())
	writeAt(t, statusPath(home), statuslineJSON(statuslnAt, 86, future), now())
	if got, _ := Load(home); got.FiveHour.Percent != 86 {
		t.Fatalf("statusline should win, got %+v", got)
	}
	home = t.TempDir()
	recent := now().Add(-time.Minute)
	writeAt(t, claudePath(home), claudeJSON(recent, 5, 12, future), now())
	writeAt(t, statusPath(home), statuslineJSON(statuslnAt, 86, future), now())
	if got, _ := Load(home); got.FiveHour.Percent != 5 {
		t.Fatalf("cached usage should win, got %+v", got)
	}
}

func TestWindowPastItsResetIsReportedAsReset(t *testing.T) {
	home := t.TempDir()
	past := now().Add(-time.Minute).Truncate(time.Second)
	writeAt(t, statusPath(home), statuslineJSON(statuslnAt, 86, past), now())
	got, ok := Load(home)
	if !ok || got.FiveHour.Percent != 0 || got.FiveHour.ResetsAt.After(now()) || got.SevenDay.Percent != 21 {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestGarbledAndMissingFilesAreAbsent(t *testing.T) {
	home := t.TempDir()
	if _, ok := Load(home); ok {
		t.Fatal("missing files should report nothing")
	}
	writeAt(t, claudePath(home), `{"cachedUsageUtilization": {`, now())
	writeAt(t, statusPath(home), `not json`, now())
	if _, ok := Load(home); ok {
		t.Fatal("garbled files should report nothing")
	}
	writeAt(t, claudePath(home), `{"cachedUsageUtilization": null}`, now().Add(time.Second))
	if _, ok := Load(home); ok {
		t.Fatal("null field should report nothing")
	}
}

func TestUnchangedMtimeIsNotReread(t *testing.T) {
	home := t.TempDir()
	mtime := now().Add(-time.Hour).Truncate(time.Second)
	writeAt(t, statusPath(home), statuslineJSON(statuslnAt, 86, future), mtime)
	Load(home)
	writeAt(t, statusPath(home), statuslineJSON(statuslnAt, 10, future), mtime)
	if got, _ := Load(home); got.FiveHour.Percent != 86 {
		t.Fatalf("file was reread: %+v", got)
	}
	writeAt(t, statusPath(home), statuslineJSON(statuslnAt, 10, future), mtime.Add(time.Second))
	if got, _ := Load(home); got.FiveHour.Percent != 10 {
		t.Fatalf("changed file was not reread: %+v", got)
	}
}
