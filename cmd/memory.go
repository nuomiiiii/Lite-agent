package cmd

import (
	"log"
	"os"
	"runtime/debug"
)

const defaultAgentMemoryLimit = 48 << 20

func configureRuntimeMemory() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultAgentMemoryLimit)
		log.Printf("runtime memory limit %dMiB", defaultAgentMemoryLimit>>20)
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
}
