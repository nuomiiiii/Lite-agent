package monitoring

import "testing"

func TestGetIPAddressCustomSkipsLookup(t *testing.T) {
	orig4, orig6 := flags.CustomIpv4, flags.CustomIpv6
	origNic := flags.GetIpAddrFromNic
	t.Cleanup(func() {
		flags.CustomIpv4 = orig4
		flags.CustomIpv6 = orig6
		flags.GetIpAddrFromNic = origNic
		publicIPCache.reset()
		nicIPCache.reset()
	})

	flags.GetIpAddrFromNic = false
	flags.CustomIpv4 = "203.0.113.9"
	flags.CustomIpv6 = "2001:db8::9"
	publicIPCache.reset()
	nicIPCache.reset()

	v4, v6, err := GetIPAddress()
	if err != nil {
		t.Fatal(err)
	}
	if v4 != "203.0.113.9" || v6 != "2001:db8::9" {
		t.Fatalf("GetIPAddress() = %s %s", v4, v6)
	}
}

func TestCachedHostIdentityStable(t *testing.T) {
	osNameCache.reset()
	kernelCache.reset()
	if first, second := CachedOSName(), CachedOSName(); first == "" || first != second {
		t.Fatalf("CachedOSName %q vs %q", first, second)
	}
	if first, second := CachedKernelVersion(), CachedKernelVersion(); first != second {
		t.Fatalf("CachedKernelVersion %q vs %q", first, second)
	}
}
