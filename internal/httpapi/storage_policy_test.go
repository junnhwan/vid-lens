package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"vidlens/internal/store"
)

func TestAdminStorageUpdatesDiskStopPercent(t *testing.T) {
	server, api := apiTest(t)
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/admin/storage", strings.NewReader(`{"expected_revision":1,"video_quota_bytes":10737418240,"image_quota_bytes":5368709120,"disk_stop_percent":92}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+api.Config.AdminToken)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var overview store.StorageOverview
	if err = json.NewDecoder(res.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || overview.Settings.DiskStopPercent != 92 || overview.Settings.Revision != 2 {
		t.Fatalf("storage policy update: status=%d settings=%+v", res.StatusCode, overview.Settings)
	}
}
