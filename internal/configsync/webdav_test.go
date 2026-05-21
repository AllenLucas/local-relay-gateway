package configsync_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"relay-gateway/internal/configsync"
)

func TestWebDAVClientUploadSnapshotUsesDeviceTimestampNameAndPrunesOldFiles(t *testing.T) {
	files := map[string][]byte{
		"/sync/allenlucasAIProxyTool/lrg-config-20260101T000000Z-old-a.json": []byte("old-a"),
		"/sync/allenlucasAIProxyTool/lrg-config-20260102T000000Z-old-b.json": []byte("old-b"),
		"/sync/allenlucasAIProxyTool/lrg-config-20260103T000000Z-old-c.json": []byte("old-c"),
		"/sync/allenlucasAIProxyTool/lrg-config-20260104T000000Z-old-d.json": []byte("old-d"),
		"/sync/allenlucasAIProxyTool/lrg-config-20260105T000000Z-old-e.json": []byte("old-e"),
	}
	var deleted []string
	var mkcolPath string
	var putPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			mkcolPath = r.URL.Path
			w.WriteHeader(http.StatusCreated)
		case "PROPFIND":
			if got := r.Header.Get("Depth"); got != "1" {
				t.Fatalf("Depth = %q, want 1", got)
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(propfindResponse(files)))
		case "PUT":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll PUT body error = %v", err)
			}
			putPath = r.URL.Path
			files[r.URL.Path] = body
			w.WriteHeader(http.StatusCreated)
		case "DELETE":
			deleted = append(deleted, r.URL.Path)
			delete(files, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	client := configsync.NewWebDAVClient(configsync.WebDAVConfig{
		BaseURL:    server.URL + "/sync/",
		DeviceName: "Work Laptop 15",
		Now: func() time.Time {
			return time.Date(2026, 5, 21, 14, 30, 45, 0, time.UTC)
		},
	})

	uploaded, err := client.UploadSnapshot(t.Context(), []byte(`{"schema_version":1}`))
	if err != nil {
		t.Fatalf("UploadSnapshot error = %v", err)
	}

	wantName := "lrg-config-20260521T143045Z-work-laptop-15.json"
	if path.Base(uploaded.Path) != wantName {
		t.Fatalf("uploaded path = %q, want base %q", uploaded.Path, wantName)
	}
	if mkcolPath != "/sync/allenlucasAIProxyTool/" {
		t.Fatalf("MKCOL path = %q, want /sync/allenlucasAIProxyTool/", mkcolPath)
	}
	if putPath != "/sync/allenlucasAIProxyTool/"+wantName {
		t.Fatalf("PUT path = %q, want /sync/allenlucasAIProxyTool/%s", putPath, wantName)
	}
	if !bytes.Equal(files["/sync/allenlucasAIProxyTool/"+wantName], []byte(`{"schema_version":1}`)) {
		t.Fatalf("uploaded body = %q", string(files["/sync/allenlucasAIProxyTool/"+wantName]))
	}
	wantDeleted := []string{"/sync/allenlucasAIProxyTool/lrg-config-20260101T000000Z-old-a.json"}
	if !reflect.DeepEqual(deleted, wantDeleted) {
		t.Fatalf("deleted = %#v, want %#v", deleted, wantDeleted)
	}
	if len(files) != 5 {
		t.Fatalf("len(files) = %d, want 5", len(files))
	}
}

func TestWebDAVClientDownloadLatestSnapshotChoosesNewestConfigFile(t *testing.T) {
	files := map[string][]byte{
		"/sync/allenlucasAIProxyTool/lrg-config-20260101T000000Z-old.json": []byte("old"),
		"/sync/allenlucasAIProxyTool/lrg-config-20260521T143045Z-new.json": []byte("new"),
		"/sync/allenlucasAIProxyTool/readme.txt":                           []byte("ignore"),
	}
	var propfindPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			propfindPath = r.URL.Path
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(propfindResponse(files)))
		case "GET":
			body, ok := files[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	client := configsync.NewWebDAVClient(configsync.WebDAVConfig{BaseURL: server.URL + "/sync/"})

	file, err := client.DownloadLatestSnapshot(t.Context())
	if err != nil {
		t.Fatalf("DownloadLatestSnapshot error = %v", err)
	}
	if path.Base(file.Path) != "lrg-config-20260521T143045Z-new.json" {
		t.Fatalf("downloaded path = %q, want newest", file.Path)
	}
	if string(file.Body) != "new" {
		t.Fatalf("body = %q, want new", string(file.Body))
	}
	if propfindPath != "/sync/allenlucasAIProxyTool/" {
		t.Fatalf("PROPFIND path = %q, want /sync/allenlucasAIProxyTool/", propfindPath)
	}
}

func TestWebDAVClientAcceptsAbsoluteHrefFromPropfind(t *testing.T) {
	files := map[string][]byte{
		"/sync/allenlucasAIProxyTool/lrg-config-20260521T143045Z-device.json": []byte("latest"),
	}
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>` + "http://" + r.Host + `/sync/allenlucasAIProxyTool/lrg-config-20260521T143045Z-device.json</d:href></d:response></d:multistatus>`))
		case "GET":
			requestedPath = r.URL.Path
			_, _ = w.Write(files[r.URL.Path])
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	client := configsync.NewWebDAVClient(configsync.WebDAVConfig{BaseURL: server.URL + "/sync/"})

	file, err := client.DownloadLatestSnapshot(t.Context())
	if err != nil {
		t.Fatalf("DownloadLatestSnapshot error = %v", err)
	}
	if requestedPath != "/sync/allenlucasAIProxyTool/lrg-config-20260521T143045Z-device.json" {
		t.Fatalf("requested path = %q", requestedPath)
	}
	if string(file.Body) != "latest" {
		t.Fatalf("body = %q, want latest", string(file.Body))
	}
}

func propfindResponse(files map[string][]byte) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="utf-8"?><d:multistatus xmlns:d="DAV:">`)
	for name := range files {
		builder.WriteString(`<d:response><d:href>`)
		builder.WriteString(name)
		builder.WriteString(`</d:href><d:propstat><d:prop><d:getlastmodified>Wed, 21 May 2026 14:30:45 GMT</d:getlastmodified></d:prop></d:propstat></d:response>`)
	}
	builder.WriteString(`</d:multistatus>`)
	return builder.String()
}
