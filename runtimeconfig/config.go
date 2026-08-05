package runtimeconfig

import "sync/atomic"

const DefaultReportInterval = 3.0

type State struct {
	MonthRotate        int
	Interval           float64
	IncludeNics        string
	ExcludeNics        string
	IncludeMountpoints string
	MemoryIncludeCache bool
	EnableGPU          bool
}

var (
	currentState atomic.Pointer[State]
	changes      = make(chan struct{}, 1)
)

func init() {
	currentState.Store(&State{Interval: DefaultReportInterval})
}

func Initialize(state State) {
	if state.Interval < 1 {
		state.Interval = DefaultReportInterval
	}
	copy := state
	currentState.Store(&copy)
	drainChanges()
}

func Snapshot() State {
	return *currentState.Load()
}

func Set(state State) bool {
	if state.Interval < 1 {
		state.Interval = DefaultReportInterval
	}
	for {
		previous := currentState.Load()
		if *previous == state {
			return false
		}
		next := state
		if currentState.CompareAndSwap(previous, &next) {
			notifyChanged()
			return true
		}
	}
}

func Changes() <-chan struct{} {
	return changes
}

func MonthRotateDay() int {
	return Snapshot().MonthRotate
}

func SetMonthRotateDay(day int) {
	state := Snapshot()
	state.MonthRotate = day
	Set(state)
}

func ReportInterval() float64 {
	return Snapshot().Interval
}

func IncludeNics() string {
	return Snapshot().IncludeNics
}

func ExcludeNics() string {
	return Snapshot().ExcludeNics
}

func IncludeMountpoints() string {
	return Snapshot().IncludeMountpoints
}

func MemoryIncludeCache() bool {
	return Snapshot().MemoryIncludeCache
}

func GPUEnabled() bool {
	return Snapshot().EnableGPU
}

func notifyChanged() {
	select {
	case changes <- struct{}{}:
	default:
	}
}

func drainChanges() {
	for {
		select {
		case <-changes:
		default:
			return
		}
	}
}
