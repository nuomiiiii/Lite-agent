package v2

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestBuildBasicInfoPayloadReportsOnlyRuntimeSafeConfig(t *testing.T) {
	day, interval := 10, 15.0
	include, exclude, mounts := "eth0", "lo", "/;/data"
	memoryCache, gpu := true, true
	payload := BuildBasicInfoPayload(map[string]interface{}{"version": "2.2.0.0"}, ConfigParams{
		MonthRotate:        &day,
		Interval:           &interval,
		IncludeNics:        &include,
		ExcludeNics:        &exclude,
		IncludeMountpoints: &mounts,
		MemoryIncludeCache: &memoryCache,
		EnableGPU:          &gpu,
	}, runtime.GOOS)

	var request struct {
		Params BasicInfoParams `json:"params"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("unmarshal basic info payload: %v", err)
	}
	if request.Params.ConfigState == nil || request.Params.Platform == "" {
		t.Fatalf("missing runtime config state: %+v", request.Params)
	}
	encoded := string(payload)
	for _, forbidden := range []string{
		"disable_web_ssh",
		"disable_auto_update",
		"ignore_unsafe_cert",
		"get_ip_addr_from_nic",
		"ghproxy",
		"dir",
		"service_name",
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("basic info payload contains installation-only field %q: %s", forbidden, encoded)
		}
	}
}

func TestBasicInfoConfigStateIsIgnoredByLegacyDecoder(t *testing.T) {
	interval := 8.0
	payload := BuildBasicInfoPayload(map[string]interface{}{"version": "2.2.0.0"}, ConfigParams{Interval: &interval}, runtime.GOOS)
	var request struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	var legacy struct {
		Info map[string]interface{} `json:"info"`
	}
	if err := json.Unmarshal(request.Params, &legacy); err != nil {
		t.Fatalf("legacy decoder rejected new fields: %v", err)
	}
	if legacy.Info["version"] != "2.2.0.0" {
		t.Fatalf("legacy info changed: %+v", legacy.Info)
	}
}
