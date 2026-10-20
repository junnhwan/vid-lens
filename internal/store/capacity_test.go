package store

import "testing"

func TestAdmitStorageKeepsReserveBoundary(t *testing.T) {
	total := int64(40) << 30
	workspace := int64(3) << 30
	images := IncomingFrameBytes
	extra := workspace + images
	reserve := DefaultHostReserveBytes
	l := storageLoad{available: reserve + extra, total: total, stop: 85, temp: workspace, images: images}
	if admitStorage(l) != nil {
		t.Fatal("available exactly at the reserve should still admit")
	}
	l.available--
	if admitStorage(l) == nil {
		t.Fatal("one byte under the reserve should refuse")
	}
}

func TestAdmissionIgnoresLegacyPercentStopLine(t *testing.T) {
	total := int64(40) << 30
	workspace := int64(3) << 30
	images := IncomingFrameBytes
	extra := workspace + images
	l := storageLoad{available: DefaultHostReserveBytes + extra, total: total, stop: 80, temp: workspace, images: images}
	if admitStorage(l) != nil {
		t.Fatal("available space at the fixed reserve boundary should admit")
	}
	l.available--
	if admitStorage(l) == nil {
		t.Fatal("one byte below the fixed reserve should refuse")
	}
}

func TestPublicCapacityOnlyDescribesServiceQuota(t *testing.T) {
	l := storageLoad{available: 100 << 30, total: 200 << 30, stop: 85, temp: 3 << 30, images: IncomingFrameBytes, usedTemp: 1 << 30, exports: 64 << 20, resTemp: 2 << 30, videoQuota: 12 << 30}
	view := buildCapacity(l)
	if view.TotalBytes != 12<<30 || view.UsedBytes != (1<<30)+(64<<20) || view.ReservedBytes != 2<<30 || view.AvailableBytes != (9<<30)-(64<<20) || view.Scope != "video_workspace" {
		t.Fatalf("wrong quota: %+v", view)
	}
	l.total, l.available = 1000<<30, 900<<30
	if other := buildCapacity(l); other != view {
		t.Fatalf("host sizes escaped: %+v %+v", view, other)
	}
}
func TestBuildCapacityDoesNotTreatLogicalQuotaAsAdmission(t *testing.T) {
	l := storageLoad{usedTemp: (10 << 30) - (3 << 30) + 1, available: 200 << 30, total: 200 << 30, stop: 85, temp: 3 << 30, images: IncomingFrameBytes}
	view := buildCapacity(l)
	if !view.Accepting || view.Level != "open" {
		t.Fatalf("logical quota blocked real disk capacity: %+v", view)
	}
	l.videoQuota = 20 << 30
	if other := buildCapacity(l); !other.Accepting {
		t.Fatalf("capacity rejected sufficient host disk space: %+v", other)
	}
}
func TestBuildCapacityPreservesHostGuardWithoutExposingHostBytes(t *testing.T) {
	l := storageLoad{available: 4 << 30, total: 40 << 30, stop: 85, temp: 3 << 30, images: IncomingFrameBytes}
	view := buildCapacity(l)
	if view.Accepting || view.TotalBytes != DefaultVideoQuota || view.AvailableBytes != DefaultVideoQuota || view.Level != "closed" {
		t.Fatalf("unsafe or leaked capacity: %+v", view)
	}
	l.available = 30 << 30
	l.usedTemp = 5 << 30
	if v := buildCapacity(l); !v.Accepting || v.Level != "open" {
		t.Fatalf("logical quota changed host admission: %+v", v)
	}
}
