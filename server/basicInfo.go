package server

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/nuomiiiii/lite-agent/dnsresolver"
	monitoring "github.com/nuomiiiii/lite-agent/monitoring/unit"
	"github.com/nuomiiiii/lite-agent/protocol/transport"
	v2 "github.com/nuomiiiii/lite-agent/protocol/v2"
	"github.com/nuomiiiii/lite-agent/update"

	pkg_flags "github.com/nuomiiiii/lite-agent/cmd/flags"
)

var flags = pkg_flags.GlobalConfig

var runtimeConfigStateUploadRequests = make(chan struct{}, 1)

func DoUploadBasicInfoWorks() {
	ticker := time.NewTicker(time.Duration(flags.InfoReportInterval) * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		err := uploadBasicInfo()
		if err != nil {
			log.Println("Error uploading basic info:", err)
		}
	}
}
func UpdateBasicInfo() {
	err := uploadBasicInfo()
	if err != nil {
		log.Println("Error uploading basic info:", err)
	} else {
		log.Println("Basic info uploaded successfully")
	}
}

func DoRuntimeConfigStateUploadWorks() {
	for range runtimeConfigStateUploadRequests {
		if err := uploadBasicInfo(); err != nil {
			log.Println("Error uploading runtime config state:", err)
			time.AfterFunc(5*time.Second, requestRuntimeConfigStateUpload)
		}
	}
}

func requestRuntimeConfigStateUpload() {
	select {
	case runtimeConfigStateUploadRequests <- struct{}{}:
	default:
	}
}

func uploadBasicInfo() error {
	return tryUploadData(buildBasicInfoMap())
}

func buildBasicInfoMap() map[string]interface{} {
	cpu := monitoring.CpuStaticInfo()

	osname := monitoring.OSName()
	kernelVersion := monitoring.KernelVersion()
	ipv4, ipv6, _ := monitoring.GetIPAddress()

	return map[string]interface{}{
		"cpu_name":               cpu.CPUName,
		"cpu_cores":              cpu.CPUCores,
		"cpu_physical_cores":     cpu.CPUPhysicalCores,
		"arch":                   cpu.CPUArchitecture,
		"os":                     osname,
		"kernel_version":         kernelVersion,
		"ipv4":                   ipv4,
		"ipv6":                   ipv6,
		"mem_total":              monitoring.Ram().Total,
		"swap_total":             monitoring.Swap().Total,
		"disk_total":             monitoring.Disk().Total,
		"gpu_name":               monitoring.GpuName(),
		"virtualization":         monitoring.Virtualized(),
		"version":                update.CurrentVersion,
		"remote_protocol":        2,
		"remote_control_enabled": pkg_flags.RemoteControlEnabled(),
	}
}

func tryUploadData(data map[string]interface{}) error {
	return tryUploadDataV2(data)
}

func tryUploadDataV2(data map[string]interface{}) error {
	endpoint := strings.TrimSuffix(flags.Endpoint, "/") + "/api/clients/v2/rpc"
	sentConfigResult := snapshotPendingConfigResult()
	payload := v2.BuildBasicInfoPayload(data, currentRuntimeConfigParams(), sentConfigResult, runtime.GOOS)
	body := payload
	compressed := false
	if !flags.DisableCompression {
		if gz, err := transport.GzipBytes(payload); err == nil {
			body = gz
			compressed = true
		}
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	authorizeAgentRequest(req, flags.Token)
	if compressed {
		req.Header.Set("Content-Encoding", "gzip")
	}

	client := dnsresolver.GetHTTPClientWithPreference(30*time.Second, flags.PreferIPVersion)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	message := string(respBody)

	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{StatusCode: resp.StatusCode, Status: resp.Status, Body: message}
	}
	if len(bytes.TrimSpace(respBody)) > 0 {
		if err := processBasicInfoResponse(respBody); err != nil {
			return err
		}
	}
	clearPendingConfigResult(sentConfigResult)
	return nil
}
