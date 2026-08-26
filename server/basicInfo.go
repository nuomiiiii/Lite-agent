package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/nuomiiiii/lite-agent/dnsresolver"
	"github.com/nuomiiiii/lite-agent/hostguard"
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
	cpu := monitoring.CpuStaticInfo()

	osname := monitoring.OSName()
	kernelVersion := monitoring.KernelVersion()
	ipv4, ipv6, _ := monitoring.GetIPAddress()
	hostguard.SetReportedAddresses(ipv4, ipv6)

	data := map[string]interface{}{
		"cpu_name":                 cpu.CPUName,
		"cpu_cores":                cpu.CPUCores,
		"cpu_physical_cores":       cpu.CPUPhysicalCores,
		"arch":                     cpu.CPUArchitecture,
		"os":                       osname,
		"kernel_version":           kernelVersion,
		"ipv4":                     ipv4,
		"ipv6":                     ipv6,
		"mem_total":                monitoring.Ram().Total,
		"swap_total":               monitoring.Swap().Total,
		"disk_total":               monitoring.Disk().Total,
		"gpu_name":                 monitoring.GpuName(),
		"virtualization":           monitoring.Virtualized(),
		"version":                  update.CurrentVersion,
		"remote_control_protected": hostguard.RemoteControlBlockedReason(flags.Endpoint) != "",
	}

	// 尝试上传完整数据
	err := tryUploadData(data)
	if err != nil {
		// 兼容 <= 1.0.2
		delete(data, "kernel_version")
		// 兼容 <= 1.2.0
		delete(data, "cpu_physical_cores")
		delete(data, "remote_control_protected")
		err = tryUploadData(data)
		if err != nil {
			return err
		}
	}
	return nil
}

func tryUploadData(data map[string]interface{}) error {
	protocolVersion := uploadProtocolVersion()
	if protocolVersion >= 2 {
		err := tryUploadDataWithProtocol(data, 2)
		if shouldFallbackToV1(2, err) {
			log.Printf("v2 basic info failed %d consecutive protocol attempts, falling back to v1", v2ProtocolFallbackThreshold)
			setConnectionProtocolVersion(1)
			return tryUploadDataWithProtocol(data, 1)
		}
		return err
	}
	return tryUploadDataWithProtocol(data, 1)
}

func tryUploadDataWithProtocol(data map[string]interface{}, protocolVersion int) error {
	endpoint := strings.TrimSuffix(flags.Endpoint, "/") + "/api/clients/uploadBasicInfo"
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var sentConfigResult *v2.ConfigResultParams
	if protocolVersion >= 2 {
		endpoint = strings.TrimSuffix(flags.Endpoint, "/") + "/api/clients/v2/rpc"
		sentConfigResult = snapshotPendingConfigResult()
		payload = v2.BuildBasicInfoPayload(data, currentRuntimeConfigParams(), sentConfigResult, runtime.GOOS)
	}
	body := payload
	compressed := false
	if protocolVersion >= 2 && !flags.DisableCompression {
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
		if err := processBasicInfoResponse(respBody, protocolVersion); err != nil {
			return err
		}
	}
	if protocolVersion >= 2 {
		clearPendingConfigResult(sentConfigResult)
		resetV2ProtocolFailures(protocolVersion)
	}

	return nil
}
