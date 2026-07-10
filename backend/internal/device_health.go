package internal

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"time"
)

const (
	deviceHealthInterval      = 2 * time.Minute  // probe cycle — don't hammer cameras
	deviceHealthOfflineSkip   = 3                // skip N cycles before re-probing known-offline devices
	pushStaleThreshold        = 5 * time.Minute  // push-SDK: offline after N minutes silence
	probeTimeout              = 8 * time.Second  // per-device ISAPI probe hard limit
)

// StartDeviceHealthScheduler runs a background loop that:
//   - Marks push-SDK (OTAP) devices offline when last_seen exceeds pushStaleThreshold.
//   - Probes ISAPI pull-mode devices (IP + credentials) every deviceHealthInterval
//     and flips their online flag to match the actual result.
//
// Returns a cancel func; call it to stop the loop cleanly.
func StartDeviceHealthScheduler(store *Store, hub *AgentHub) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Short warm-up so the server finishes starting before the first sweep.
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
		runDeviceHealthCheck(ctx, store, hub)

		ticker := time.NewTicker(deviceHealthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runDeviceHealthCheck(ctx, store, hub)
			}
		}
	}()
	return cancel
}

var healthCycle int

func runDeviceHealthCheck(ctx context.Context, store *Store, hub *AgentHub) {
	healthCycle++
	devices, err := store.ListDevices(ctx)
	if err != nil {
		log.Printf("[device-health] list: %v", err)
		return
	}
	for _, d := range devices {
		d := d
		// Already-offline ISAPI devices: probe every Nth cycle to avoid flooding.
		if !d.Online && d.IP != "" && d.Reach != "otap" && healthCycle%deviceHealthOfflineSkip != 0 {
			continue
		}
		go checkOneDevice(ctx, store, hub, d)
	}
}

func checkOneDevice(ctx context.Context, store *Store, hub *AgentHub, d Device) {
	// Push-SDK (OTAP) / no-IP devices: no outbound probe — just stale detection.
	if d.Reach == "otap" || (d.IP == "" && d.ISAPIUsername == "") {
		if d.Online && d.LastSeen != nil && time.Since(*d.LastSeen) > pushStaleThreshold {
			_ = store.SetDeviceOffline(ctx, d.DeviceID)
			log.Printf("[device-health] %s: push-SDK stale (%s since last contact), marked offline",
				d.DeviceID, time.Since(*d.LastSeen).Round(time.Second))
		}
		return
	}

	// ISAPI pull-mode: must have IP and credentials to probe.
	if d.IP == "" || d.ISAPIUsername == "" {
		return
	}

	// ListDevices omits the ISAPI password; reload the full device record.
	full, err := store.GetDevice(ctx, d.DeviceID)
	if err != nil || full == nil {
		return
	}

	client := newProbeClient(full, hub)
	info, probeErr := client.GetDeviceInfo()
	online := probeErr == nil

	model, firmware := d.Model, d.Firmware
	if online && info != nil {
		if info.Model != "" {
			model = info.Model
		}
		if info.FirmwareVersion != "" {
			firmware = info.FirmwareVersion
		}
	}

	if d.Online != online {
		log.Printf("[device-health] %s (%s): %s → %s",
			d.DeviceID, d.Name, onlineWord(d.Online), onlineWord(online))
	}
	_ = store.SetDeviceOnline(ctx, d.DeviceID, model, firmware, online)
}

// newProbeClient builds a short-timeout ISAPI client for health checks.
func newProbeClient(d *Device, hub *AgentHub) *ISAPIClient {
	c := isapiClientDirect(d, hub)
	c.HTTPClient = &http.Client{
		Timeout: probeTimeout,
		Transport: &http.Transport{
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			ResponseHeaderTimeout: probeTimeout - time.Second,
			DisableKeepAlives:     true,
		},
	}
	return c
}

func onlineWord(b bool) string {
	if b {
		return "online"
	}
	return "offline"
}
