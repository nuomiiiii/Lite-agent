package v2

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
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
	}, nil, runtime.GOOS)

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
		"allow_control_node_remote",
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
	payload := BuildBasicInfoPayload(map[string]interface{}{"version": "2.2.0.0"}, ConfigParams{Interval: &interval}, nil, runtime.GOOS)
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

func TestBuildBasicInfoPayloadCarriesConfigResult(t *testing.T) {
	result := &ConfigResultParams{Revision: 9, EventID: "event-9", Status: "failed", Error: "bad interface"}
	payload := BuildBasicInfoPayload(map[string]interface{}{"version": "2.2.0.1"}, ConfigParams{Revision: 8}, result, runtime.GOOS)
	var request Request
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	var params BasicInfoParams
	if err := BindParams(request.Params, &params); err != nil {
		t.Fatalf("bind params: %v", err)
	}
	if params.ConfigResult == nil || *params.ConfigResult != *result {
		t.Fatalf("config result = %+v, want %+v", params.ConfigResult, result)
	}
	if params.ConfigState == nil || params.ConfigState.Revision != 8 {
		t.Fatalf("config state = %+v", params.ConfigState)
	}
}

func TestBuildRouteResultPayloadIncludesReachability(t *testing.T) {
	finished := time.Unix(0, 0).UTC()
	payload := BuildRouteResultPayload(RouteParams{TaskID: 3, Protocol: "icmp", Target: "example.com", IPVersion: 4}, nil, "", finished, "1.1.1.1", true)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"resolved_target_ip":"1.1.1.1"`) {
		t.Fatalf("missing resolved_target_ip: %s", text)
	}
	if !strings.Contains(text, `"target_reached":true`) {
		t.Fatalf("missing target_reached: %s", text)
	}
}

func TestBuildRouteResultPayloadOmitsEmptyReachability(t *testing.T) {
	finished := time.Unix(0, 0).UTC()
	payload := BuildRouteResultPayload(RouteParams{TaskID: 3, Target: "1.1.1.1", IPVersion: 4}, nil, "", finished, "", false)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "resolved_target_ip") || strings.Contains(text, "target_reached") {
		t.Fatalf("optional fields should be omitted: %s", text)
	}
}
