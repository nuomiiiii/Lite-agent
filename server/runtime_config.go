package server

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nuomiiiii/lite-agent/monitoring/netstatic"
	monitoring "github.com/nuomiiiii/lite-agent/monitoring/unit"
	v2 "github.com/nuomiiiii/lite-agent/protocol/v2"
	"github.com/nuomiiiii/lite-agent/runtimeconfig"
	"github.com/nuomiiiii/lite-agent/utils"
)

type runtimeConfigEnvelope struct {
	Config             *v2.ConfigParams `json:"config,omitempty"`
	RequestConfigState bool             `json:"request_config_state,omitempty"`
}

func processBasicInfoResponse(body []byte) error {
	var envelope runtimeConfigEnvelope
	response, err := parseV2Response(body)
	if err != nil {
		return err
	}
	if response.Result == nil {
		return nil
	}
	if err := v2.BindResult(response.Result, &envelope); err != nil {
		return fmt.Errorf("failed to parse runtime config: %w", err)
	}
	if envelope.Config == nil {
		if envelope.RequestConfigState {
			return tryUploadData(map[string]interface{}{
				"month_rotate":          runtimeconfig.MonthRotateDay(),
				"month_rotate_time":     runtimeconfig.MonthRotateTime(),
				"month_rotate_timezone": runtimeconfig.MonthRotateTimezone(),
			})
		}
		return nil
	}
	processRuntimeConfig(*envelope.Config, "")
	return nil
}

func currentRuntimeConfigParams() v2.ConfigParams {
	state := runtimeconfig.Snapshot()
	return v2.ConfigParams{
		Revision:            appliedRuntimeConfigRevision(),
		MonthRotate:         &state.MonthRotate,
		MonthRotateTime:     &state.MonthRotateTime,
		MonthRotateTimezone: &state.MonthRotateTimezone,
		Interval:            &state.Interval,
		IncludeNics:         &state.IncludeNics,
		ExcludeNics:         &state.ExcludeNics,
		IncludeMountpoints:  &state.IncludeMountpoints,
		MemoryIncludeCache:  &state.MemoryIncludeCache,
		EnableGPU:           &state.EnableGPU,
	}
}

func applyRuntimeConfig(config v2.ConfigParams) (bool, error) {
	current := runtimeconfig.Snapshot()
	next := current
	var err error

	if config.MonthRotate != nil {
		if *config.MonthRotate < 0 || *config.MonthRotate > 31 {
			return false, fmt.Errorf("month_rotate must be 0 or a day from 1 to 31")
		}
		next.MonthRotate = *config.MonthRotate
	}
	if config.MonthRotateTime != nil {
		next.MonthRotateTime, err = validateRuntimeText("month_rotate_time", *config.MonthRotateTime, 16)
		if err != nil {
			return false, err
		}
		if next.MonthRotateTime != "" {
			if _, _, _, err = utils.ParseResetClock(next.MonthRotateTime); err != nil {
				return false, fmt.Errorf("month_rotate_time must be HH:MM or HH:MM:SS")
			}
		}
	}
	if config.MonthRotateTimezone != nil {
		next.MonthRotateTimezone, err = validateRuntimeText("month_rotate_timezone", *config.MonthRotateTimezone, 64)
		if err != nil {
			return false, err
		}
		if next.MonthRotateTimezone != "" {
			if _, err = time.LoadLocation(next.MonthRotateTimezone); err != nil {
				return false, fmt.Errorf("unknown month_rotate_timezone %q", next.MonthRotateTimezone)
			}
		}
	}
	if config.Interval != nil {
		if *config.Interval < 1 || *config.Interval > 3600 {
			return false, fmt.Errorf("interval must be between 1 and 3600 seconds")
		}
		next.Interval = *config.Interval
	}
	if config.IncludeNics != nil {
		next.IncludeNics, err = validateRuntimeText("include_nics", *config.IncludeNics, 1024)
		if err != nil {
			return false, err
		}
	}
	if config.ExcludeNics != nil {
		next.ExcludeNics, err = validateRuntimeText("exclude_nics", *config.ExcludeNics, 1024)
		if err != nil {
			return false, err
		}
	}
	if config.IncludeMountpoints != nil {
		next.IncludeMountpoints, err = validateRuntimeText("include_mountpoints", *config.IncludeMountpoints, 2048)
		if err != nil {
			return false, err
		}
	}
	if config.MemoryIncludeCache != nil {
		next.MemoryIncludeCache = *config.MemoryIncludeCache
	}
	if config.EnableGPU != nil {
		next.EnableGPU = *config.EnableGPU
	}
	if next == current {
		return false, nil
	}

	networkConfigChanged := next.MonthRotate != current.MonthRotate ||
		next.MonthRotateTime != current.MonthRotateTime ||
		next.MonthRotateTimezone != current.MonthRotateTimezone ||
		next.IncludeNics != current.IncludeNics || next.ExcludeNics != current.ExcludeNics
	if networkConfigChanged {
		if next.MonthRotate == 0 {
			if err := netstatic.Stop(); err != nil {
				return false, fmt.Errorf("stop network statistics: %w", err)
			}
		} else {
			if err := netstatic.StartOrContinue(); err != nil {
				return false, fmt.Errorf("start network statistics: %w", err)
			}
			nics, err := monitoring.InterfaceListForFilters(next.IncludeNics, next.ExcludeNics)
			if err != nil {
				return false, fmt.Errorf("list network interfaces: %w", err)
			}
			if err := netstatic.SetNewConfig(netstatic.NetStaticConfig{Nics: nics}); err != nil {
				return false, fmt.Errorf("configure network statistics: %w", err)
			}
		}
	}

	runtimeconfig.Set(next)
	log.Printf("Applied Lite runtime config: interval=%gs month_rotate=%d time=%s tz=%s", next.Interval, next.MonthRotate, next.MonthRotateTime, next.MonthRotateTimezone)
	return true, nil
}

func validateRuntimeText(field, value string, maxLength int) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > maxLength {
		return "", fmt.Errorf("%s is too long", field)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("%s must not contain control characters", field)
	}
	return value, nil
}
