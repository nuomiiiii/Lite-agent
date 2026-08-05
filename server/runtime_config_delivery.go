package server

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	v2 "github.com/komari-monitor/komari-agent/protocol/v2"
)

const configResultErrorLimit = 512

var (
	runtimeConfigApplyMu    sync.Mutex
	processedConfigRevision atomic.Uint64
	appliedConfigRevision   atomic.Uint64
	pendingConfigResultMu   sync.Mutex
	pendingConfigResult     *v2.ConfigResultParams
)

func processRuntimeConfig(config v2.ConfigParams, eventID string) bool {
	runtimeConfigApplyMu.Lock()
	defer runtimeConfigApplyMu.Unlock()

	if config.Revision > 0 && config.Revision <= processedConfigRevision.Load() {
		return true
	}

	changed, err := applyRuntimeConfig(config)
	if config.Revision == 0 {
		if err != nil {
			log.Printf("failed to apply unversioned runtime config: %v", err)
			return false
		}
		if changed {
			requestRuntimeConfigStateUpload()
		}
		return true
	}

	processedConfigRevision.Store(config.Revision)
	result := v2.ConfigResultParams{
		Revision: config.Revision,
		EventID:  eventID,
		Status:   "applied",
	}
	if err != nil {
		result.Status = "failed"
		result.Error = sanitizeConfigResultError(err.Error())
		log.Printf("failed to apply runtime config revision %d: %v", config.Revision, err)
	} else {
		appliedConfigRevision.Store(config.Revision)
	}
	setPendingConfigResult(result)
	requestRuntimeConfigStateUpload()
	return true
}

func appliedRuntimeConfigRevision() uint64 {
	return appliedConfigRevision.Load()
}

func sanitizeConfigResultError(message string) string {
	message = strings.TrimSpace(message)
	message = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, message)
	if len(message) > configResultErrorLimit {
		for len(message) > configResultErrorLimit {
			_, size := utf8.DecodeLastRuneInString(message)
			if size <= 0 {
				message = message[:configResultErrorLimit]
				break
			}
			message = message[:len(message)-size]
		}
	}
	return message
}

func setPendingConfigResult(result v2.ConfigResultParams) {
	pendingConfigResultMu.Lock()
	defer pendingConfigResultMu.Unlock()
	if pendingConfigResult != nil && pendingConfigResult.Revision > result.Revision {
		return
	}
	copy := result
	pendingConfigResult = &copy
}

func snapshotPendingConfigResult() *v2.ConfigResultParams {
	pendingConfigResultMu.Lock()
	defer pendingConfigResultMu.Unlock()
	if pendingConfigResult == nil {
		return nil
	}
	copy := *pendingConfigResult
	return &copy
}

func clearPendingConfigResult(sent *v2.ConfigResultParams) {
	if sent == nil {
		return
	}
	pendingConfigResultMu.Lock()
	defer pendingConfigResultMu.Unlock()
	if pendingConfigResult != nil &&
		pendingConfigResult.Revision == sent.Revision &&
		pendingConfigResult.Status == sent.Status &&
		pendingConfigResult.EventID == sent.EventID {
		pendingConfigResult = nil
	}
}
