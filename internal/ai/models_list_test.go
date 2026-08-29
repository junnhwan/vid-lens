package ai

import (
	"testing"
)

func TestNormalizeModelsBaseURL(t *testing.T) {
	got, err := normalizeModelsBaseURL("https://api.example.com/v1/embeddings")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.com/v1" {
		t.Fatalf("got %q", got)
	}
}

func TestParseOpenAIModelsResponse(t *testing.T) {
	ids, err := parseOpenAIModelsResponse([]byte(`{"data":[{"id":"b"},{"id":"a"},{"id":"a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("got %#v", ids)
	}
}

func TestValidatePublicHTTPURLRejectsLocal(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/v1",
		"http://localhost/v1",
		"http://10.0.0.1/v1",
		"http://169.254.169.254/latest",
		"ftp://example.com/v1",
	}
	for _, c := range cases {
		if err := validatePublicHTTPURL(c); err == nil {
			t.Fatalf("expected reject for %s", c)
		}
	}
	if err := validatePublicHTTPURL("https://api.siliconflow.cn/v1"); err != nil {
		t.Fatal(err)
	}
}
