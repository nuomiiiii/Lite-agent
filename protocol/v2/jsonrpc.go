package v2

import (
	"encoding/json"
	"time"

	v1 "github.com/komari-monitor/komari-agent/protocol/v1"
)

const (
	Version                = "2.0"
	MethodAgentReport      = "agent.report"
	MethodAgentBasicInfo   = "agent.basicInfo"
	MethodAgentPingResult  = "agent.pingResult"
	MethodAgentRouteResult = "agent.routeResult"
	MethodAgentTaskResult  = "agent.taskResult"
	MethodAgentExec        = "agent.exec"
	MethodAgentPing        = "agent.ping"
	MethodAgentRoute       = "agent.route"
	MethodAgentMessage     = "agent.message"
	MethodAgentEvent       = "agent.event"
	MethodAgentTerminal    = "agent.terminal.request"
	MethodAgentRemote      = "agent.remote.request"
	MethodAgentConfig      = "agent.config"
	MethodAgentPull        = "agent.pull"
)

type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type Event struct {
	ID        string      `json:"id"`
	Method    string      `json:"method"`
	Params    interface{} `json:"params,omitempty"`
	CreatedAt string      `json:"created_at,omitempty"`
	ExpiresAt string      `json:"expires_at,omitempty"`
}

type EventResult struct {
	Status string  `json:"status,omitempty"`
	Events []Event `json:"events,omitempty"`
}

type ConfigParams struct {
	MonthRotate        *int     `json:"month_rotate,omitempty"`
	Interval           *float64 `json:"interval,omitempty"`
	IncludeNics        *string  `json:"include_nics,omitempty"`
	ExcludeNics        *string  `json:"exclude_nics,omitempty"`
	IncludeMountpoints *string  `json:"include_mountpoints,omitempty"`
	MemoryIncludeCache *bool    `json:"memory_include_cache,omitempty"`
	EnableGPU          *bool    `json:"enable_gpu,omitempty"`
}

type BasicInfoParams struct {
	Info        map[string]interface{} `json:"info"`
	ConfigState *ConfigParams          `json:"config_state,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
}

type RouteHop struct {
	TTL       int     `json:"ttl"`
	IP        string  `json:"ip,omitempty"`
	LatencyMS float64 `json:"latency_ms,omitempty"`
	Timeout   bool    `json:"timeout,omitempty"`
}

type RouteParams struct {
	TaskID    uint   `json:"task_id"`
	Protocol  string `json:"protocol"`
	Target    string `json:"target"`
	IPVersion int    `json:"ip_version"`
	MaxHops   int    `json:"max_hops"`
}

type RemoteRequestParams struct {
	RequestID string `json:"request_id"`
	Ticket    string `json:"ticket"`
}

func NewNotification(method string, params interface{}) []byte {
	payload, _ := json.Marshal(Request{JSONRPC: Version, Method: method, Params: params})
	return payload
}

func NewRequest(id interface{}, method string, params interface{}) []byte {
	payload, _ := json.Marshal(Request{JSONRPC: Version, Method: method, Params: params, ID: id})
	return payload
}

func BuildReportPayload(report v1.ReportPayload) []byte {
	return NewNotification(MethodAgentReport, reportParams{Report: json.RawMessage(report)})
}

func BuildReportRequest(id interface{}, report v1.ReportPayload, ackEventIDs []string) []byte {
	return NewRequest(id, MethodAgentReport, reportParams{Report: json.RawMessage(report), AckEventIDs: ackEventIDs})
}

func BuildBasicInfoPayload(info map[string]interface{}, configState ConfigParams, platform string) []byte {
	return NewNotification(MethodAgentBasicInfo, BasicInfoParams{
		Info:        info,
		ConfigState: &configState,
		Platform:    platform,
	})
}

type reportParams struct {
	Report      json.RawMessage `json:"report"`
	AckEventIDs []string        `json:"ack_event_ids,omitempty"`
}

func BuildPingResultPayload(taskID uint, pingType string, value int, finishedAt time.Time) interface{} {
	return Request{
		JSONRPC: Version,
		Method:  MethodAgentPingResult,
		Params: map[string]interface{}{
			"task_id":     taskID,
			"ping_type":   pingType,
			"value":       value,
			"finished_at": finishedAt.Format(time.RFC3339Nano),
		},
	}
}

func BuildRouteResultPayload(task RouteParams, hops []RouteHop, probeError string, finishedAt time.Time) interface{} {
	return Request{
		JSONRPC: Version,
		Method:  MethodAgentRouteResult,
		Params: map[string]interface{}{
			"task_id": task.TaskID, "protocol": task.Protocol, "target": task.Target,
			"ip_version": task.IPVersion, "hops": hops, "error": probeError,
			"finished_at": finishedAt.Format(time.RFC3339Nano),
		},
	}
}

func BindParams(raw interface{}, target interface{}) error {
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

func BindResult(raw interface{}, target interface{}) error {
	return BindParams(raw, target)
}
