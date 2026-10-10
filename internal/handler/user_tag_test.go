package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestParseTagFilterRejectsEmptyAndUnknown(t *testing.T) {
	for _, query := range []string{"tag_ids=", "tag_ids=a,,b", "tag_ids=a&tag_ids=b", "tag_match=none", "tag_match=any&tag_match=all"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/media/list?"+query, nil)
		if _, _, err := parseTagFilter(c); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/media/list?tag_ids=a,b&tag_match=any", nil)
	ids, match, err := parseTagFilter(c)
	if err != nil || len(ids) != 2 || match != "any" {
		t.Fatal(ids, match, err)
	}
}
