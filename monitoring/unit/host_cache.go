package monitoring

var (
	osNameCache  ttlCache[string]
	kernelCache  ttlCache[string]
	gpuNameCache ttlCache[string]
	virtCache    ttlCache[string]
)

func CachedOSName() string {
	return osNameCache.get(0, OSName)
}

func CachedKernelVersion() string {
	return kernelCache.get(0, KernelVersion)
}

func CachedGpuName() string {
	return gpuNameCache.get(0, GpuName)
}

func CachedVirtualized() string {
	return virtCache.get(0, Virtualized)
}
