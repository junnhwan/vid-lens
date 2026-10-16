package store

import "testing"

func TestAdmitStorageKeepsReserveBoundary(t *testing.T) {
	total := int64(40) << 30
	workspace := int64(3) << 30
	images := IncomingFrameBytes
	extra := workspace + images
	reserve := int64(6) << 30
	l := storageLoad{available: reserve + extra, total: total, stop: 85, temp: workspace, images: images}
	if admitStorage(l) != nil {
		t.Fatal("available exactly at the reserve should still admit")
	}
	l.available--
	if admitStorage(l) == nil {
		t.Fatal("one byte under the reserve should refuse")
	}
}

func TestAdmitStorageKeepsStopPercentBoundary(t *testing.T) {
	total := int64(40) << 30
	workspace := int64(3) << 30
	images := IncomingFrameBytes
	extra := workspace + images
	used := total*85/100 - extra
	l := storageLoad{available: total - used, total: total, stop: 85, temp: workspace, images: images}
	if admitStorage(l) != nil {
		t.Fatal("used exactly at the stop percent should still admit")
	}
	l.available--
	if admitStorage(l) == nil {
		t.Fatal("one byte past the stop percent should refuse")
	}
}

func TestBuildCapacityUsesEarlierFloorWhenStopIsLooser(t *testing.T) {
	total := int64(40) << 30
	workspace := int64(3) << 30
	extra := workspace + IncomingFrameBytes
	l := storageLoad{available: total, total: total, stop: 95, temp: workspace, images: IncomingFrameBytes}
	view := buildCapacity(l)
	want := total - extra - (5 << 30)
	if view.CeilingBytes != want {
		t.Fatalf("ceiling = %d, want %d", view.CeilingBytes, want)
	}
	if !view.Accepting || view.Level != "open" || view.BlockedBy != "" {
		t.Fatalf("empty volume should accept, got %+v", view)
	}
}

func TestBuildCapacityClosesOnMediaLedgerBeforeDisk(t *testing.T) {
	total := int64(200) << 30
	workspace := int64(3) << 30
	l := storageLoad{
		usedTemp:  (10 << 30) - workspace + 1,
		available: total,
		total:     total,
		stop:      85,
		temp:      workspace,
		images:    IncomingFrameBytes,
	}
	view := buildCapacity(l)
	if view.Accepting || view.Level != "closed" || view.BlockedBy != "media" {
		t.Fatalf("full temporary ledger should close without blaming the volume, got %+v", view)
	}
}

func TestBuildCapacityTightensNearTheNextDownload(t *testing.T) {
	total := int64(40) << 30
	workspace := int64(3) << 30
	extra := workspace + IncomingFrameBytes
	reserve := diskFloor(total, 85)
	ceiling := total - extra - reserve
	used := ceiling - workspace/2
	l := storageLoad{available: total - used, total: total, stop: 85, temp: workspace, images: IncomingFrameBytes}
	view := buildCapacity(l)
	if !view.Accepting || view.Level != "tight" {
		t.Fatalf("less than one workspace before the line should be tight, got %+v", view)
	}
}
