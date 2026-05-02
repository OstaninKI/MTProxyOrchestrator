package component

import (
	"net/http"
	"testing"
)

func TestHTTPClientAppliesDefaultTimeoutToZeroTimeoutHTTPClient(t *testing.T) {
	dl := Downloader{Client: &http.Client{}}

	client, ok := dl.httpClient().(*http.Client)
	if !ok {
		t.Fatal("expected httpClient to return *http.Client")
	}
	if client.Timeout != DefaultHTTPTimeout {
		t.Fatalf("timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
	}
}
