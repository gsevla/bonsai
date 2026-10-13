package format

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func Size(bytes int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(bytes)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func ParseSize(text string) (int64, error) {
	upper := strings.ToUpper(strings.TrimSpace(text))
	upper = strings.TrimSuffix(strings.TrimSuffix(upper, "B"), "I")
	multiplier := int64(1)
	if upper != "" {
		if shift := strings.IndexByte("KMGT", upper[len(upper)-1]); shift >= 0 {
			multiplier = 1 << (10 * (shift + 1))
			upper = upper[:len(upper)-1]
		}
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(upper), 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid size %q, expected something like 500M or 2G", text)
	}
	return int64(value * float64(multiplier)), nil
}

func Ago(then, now time.Time) string {
	if then.IsZero() {
		return "-"
	}
	elapsed := now.Sub(then)
	days := int(elapsed.Hours() / 24)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return plural(int(elapsed.Minutes()), "minute")
	case days < 1:
		return plural(int(elapsed.Hours()), "hour")
	case days == 1:
		return "yesterday"
	case days < 14:
		return plural(days, "day")
	case days < 60:
		return plural(days/7, "week")
	case days < 730:
		return plural(days/30, "month")
	}
	return plural(days/365, "year")
}

func plural(count int, unit string) string {
	if count == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", count, unit)
}
