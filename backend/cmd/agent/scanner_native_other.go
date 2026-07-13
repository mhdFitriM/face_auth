//go:build !windows

package main

import "context"

func startNativeScannerManager(_ context.Context, _ []scannerRoute) error { return nil }
func refreshPhysicalScanners() []PhysicalScanner {
	devices, _ := scannerSnapshot()
	return devices
}
