package utils

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

const DefaultResetTimezone = "Asia/Shanghai"

// GetLastResetDate 计算上一个重置日期（北京时间当天 0:00）。
func GetLastResetDate(resetDay int, currentDate time.Time) time.Time {
	return GetLastResetInstant(resetDay, "", "", currentDate)
}

// GetLastResetInstant 按指定时区与时刻计算上一次流量重置。
// 时区或时刻为空时与 Lite 存量升级一致：Asia/Shanghai + 00:00:00。
func GetLastResetInstant(resetDay int, clock, timezone string, currentDate time.Time) time.Time {
	if resetDay < 1 || resetDay > 31 {
		return currentDate
	}
	hour, minute, second, err := ParseResetClock(clock)
	if err != nil {
		hour, minute, second = 0, 0, 0
	}
	loc := ResetLocation(timezone)
	local := currentDate.In(loc)
	this := actualResetInstant(local.Year(), local.Month(), resetDay, hour, minute, second, loc)
	if !local.Before(this) {
		return this
	}
	prevMonth := local.Month() - 1
	prevYear := local.Year()
	if prevMonth < 1 {
		prevMonth = 12
		prevYear--
	}
	return actualResetInstant(prevYear, prevMonth, resetDay, hour, minute, second, loc)
}

func ParseResetClock(value string) (hour, minute, second int, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, 0, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid clock")
	}
	if _, err = fmt.Sscanf(parts[0], "%d", &hour); err != nil {
		return 0, 0, 0, err
	}
	if _, err = fmt.Sscanf(parts[1], "%d", &minute); err != nil {
		return 0, 0, 0, err
	}
	if len(parts) == 3 {
		if _, err = fmt.Sscanf(parts[2], "%d", &second); err != nil {
			return 0, 0, 0, err
		}
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 {
		return 0, 0, 0, fmt.Errorf("clock out of range")
	}
	return hour, minute, second, nil
}

func ResetLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || name == DefaultResetTimezone || name == "Asia/Chongqing" || name == "PRC" {
		return time.FixedZone(DefaultResetTimezone, 8*60*60)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone(DefaultResetTimezone, 8*60*60)
	}
	return loc
}

func actualResetInstant(year int, month time.Month, resetDay, hour, minute, second int, location *time.Location) time.Time {
	firstDayOfNextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, location)
	lastDayOfMonth := firstDayOfNextMonth.AddDate(0, 0, -1).Day()
	day := resetDay
	if day > lastDayOfMonth {
		day = lastDayOfMonth
	}
	return time.Date(year, month, day, hour, minute, second, 0, location)
}
