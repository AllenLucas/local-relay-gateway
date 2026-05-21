package configsync

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const snapshotFilePrefix = "lrg-config-"
const snapshotFileSuffix = ".json"
const maxRemoteSnapshots = 5
const syncDirectoryName = "allenlucasAIProxyTool"

type WebDAVConfig struct {
	BaseURL    string
	Username   string
	Password   string
	DeviceName string
	Now        func() time.Time
	HTTPClient *http.Client
}

type WebDAVFile struct {
	Path       string
	Body       []byte
	ModifiedAt time.Time
}

type WebDAVClient struct {
	cfg        WebDAVConfig
	httpClient *http.Client
}

func NewWebDAVClient(cfg WebDAVConfig) *WebDAVClient {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &WebDAVClient{cfg: cfg, httpClient: httpClient}
}

func (c *WebDAVClient) UploadSnapshot(ctx context.Context, body []byte) (WebDAVFile, error) {
	baseURL, err := normalizedBaseURL(c.cfg.BaseURL)
	if err != nil {
		return WebDAVFile{}, err
	}
	if err := c.mkcol(ctx, baseURL); err != nil {
		return WebDAVFile{}, err
	}

	now := c.now().UTC()
	filename := snapshotFilename(c.cfg.DeviceName, now)
	targetURL := *baseURL
	targetURL.Path = path.Join(baseURL.Path, filename)
	if strings.HasSuffix(baseURL.Path, "/") && !strings.HasPrefix(filename, "/") {
		targetURL.Path = strings.TrimRight(baseURL.Path, "/") + "/" + filename
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, targetURL.String(), bytes.NewReader(body))
	if err != nil {
		return WebDAVFile{}, err
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return WebDAVFile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WebDAVFile{}, fmt.Errorf("webdav PUT failed: %s", resp.Status)
	}

	files, err := c.listSnapshotFiles(ctx, baseURL)
	if err != nil {
		return WebDAVFile{}, err
	}
	if !containsWebDAVPath(files, targetURL.Path) {
		files = append(files, WebDAVFile{Path: targetURL.Path, ModifiedAt: now})
	}
	if err := c.pruneOldSnapshots(ctx, baseURL, files); err != nil {
		return WebDAVFile{}, err
	}
	return WebDAVFile{Path: targetURL.Path, Body: body, ModifiedAt: now}, nil
}

func (c *WebDAVClient) DownloadLatestSnapshot(ctx context.Context) (WebDAVFile, error) {
	baseURL, err := normalizedBaseURL(c.cfg.BaseURL)
	if err != nil {
		return WebDAVFile{}, err
	}
	files, err := c.listSnapshotFiles(ctx, baseURL)
	if err != nil {
		return WebDAVFile{}, err
	}
	if len(files) == 0 {
		return WebDAVFile{}, errors.New("no remote config snapshots found")
	}
	sortSnapshotFiles(files)
	latest := files[len(files)-1]

	fileURL := *baseURL
	fileURL.Path = latest.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL.String(), nil)
	if err != nil {
		return WebDAVFile{}, err
	}
	c.authorize(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return WebDAVFile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WebDAVFile{}, fmt.Errorf("webdav GET failed: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return WebDAVFile{}, err
	}
	latest.Body = body
	return latest, nil
}

func (c *WebDAVClient) mkcol(ctx context.Context, baseURL *url.URL) error {
	req, err := http.NewRequestWithContext(ctx, "MKCOL", baseURL.String(), nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("webdav MKCOL failed: %s", resp.Status)
}

func (c *WebDAVClient) listSnapshotFiles(ctx context.Context, baseURL *url.URL) ([]WebDAVFile, error) {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", baseURL.String(), strings.NewReader(propfindBody))
	if err != nil {
		return nil, err
	}
	c.authorize(req)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webdav PROPFIND failed: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed propfindMultistatus
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	var files []WebDAVFile
	for _, response := range parsed.Responses {
		href := strings.TrimSpace(response.Href)
		if href == "" || !isSnapshotFilename(path.Base(href)) {
			continue
		}
		href = webDAVPath(href)
		modifiedAt, _ := http.ParseTime(strings.TrimSpace(response.Propstat.Prop.GetLastModified))
		files = append(files, WebDAVFile{Path: href, ModifiedAt: modifiedAt})
	}
	sortSnapshotFiles(files)
	return files, nil
}

func (c *WebDAVClient) pruneOldSnapshots(ctx context.Context, baseURL *url.URL, files []WebDAVFile) error {
	sortSnapshotFiles(files)
	for len(files) > maxRemoteSnapshots {
		victim := files[0]
		files = files[1:]
		fileURL := *baseURL
		fileURL.Path = victim.Path
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fileURL.String(), nil)
		if err != nil {
			return err
		}
		c.authorize(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("webdav DELETE failed: %s", resp.Status)
		}
	}
	return nil
}

func (c *WebDAVClient) authorize(req *http.Request) {
	if c.cfg.Username != "" || c.cfg.Password != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
}

func (c *WebDAVClient) now() time.Time {
	if c.cfg.Now != nil {
		return c.cfg.Now()
	}
	return time.Now()
}

func normalizedBaseURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("webdav url is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("webdav url must start with http:// or https://")
	}
	if parsed.Host == "" {
		return nil, errors.New("webdav url host is empty")
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + syncDirectoryName + "/"
	return parsed, nil
}

func snapshotFilename(deviceName string, now time.Time) string {
	slug := slugify(deviceName)
	if slug == "" {
		slug = "device"
	}
	return snapshotFilePrefix + now.UTC().Format("20060102T150405Z") + "-" + slug + snapshotFileSuffix
}

func isSnapshotFilename(name string) bool {
	return strings.HasPrefix(name, snapshotFilePrefix) && strings.HasSuffix(name, snapshotFileSuffix)
}

func sortSnapshotFiles(files []WebDAVFile) {
	sort.Slice(files, func(i int, j int) bool {
		leftTime := snapshotTime(files[i])
		rightTime := snapshotTime(files[j])
		if !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		return files[i].Path < files[j].Path
	})
}

func containsWebDAVPath(files []WebDAVFile, targetPath string) bool {
	for _, file := range files {
		if file.Path == targetPath {
			return true
		}
	}
	return false
}

func webDAVPath(href string) string {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path == "" {
		return href
	}
	return parsed.EscapedPath()
}

func snapshotTime(file WebDAVFile) time.Time {
	base := path.Base(file.Path)
	if len(base) >= len(snapshotFilePrefix)+len("20060102T150405Z") {
		raw := base[len(snapshotFilePrefix) : len(snapshotFilePrefix)+len("20060102T150405Z")]
		if parsed, err := time.Parse("20060102T150405Z", raw); err == nil {
			return parsed
		}
	}
	return file.ModifiedAt
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	lower = nonSlugChars.ReplaceAllString(lower, "-")
	return strings.Trim(lower, "-")
}

type propfindMultistatus struct {
	Responses []propfindResponse `xml:"response"`
}

type propfindResponse struct {
	Href     string           `xml:"href"`
	Propstat propfindPropstat `xml:"propstat"`
}

type propfindPropstat struct {
	Prop propfindProp `xml:"prop"`
}

type propfindProp struct {
	GetLastModified string `xml:"getlastmodified"`
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:getlastmodified/>
  </d:prop>
</d:propfind>`
