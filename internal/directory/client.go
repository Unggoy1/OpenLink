package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"halocommunity/internal/api"
)

// Client talks to a directory over HTTP(S).
type Client struct {
	Base        string // e.g. https://dir.example.org
	RegisterKey string
	HTTP        *http.Client
}

// NewClient returns a client with sane timeouts. It follows a redirect only
// to the same scheme and host, so an https directory cannot be downgraded to
// http or handed off to another server (with the register key or a token).
func NewClient(base, registerKey string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), RegisterKey: registerKey,
		HTTP: &http.Client{Timeout: 10 * time.Second, CheckRedirect: sameOrigin}}
}

func sameOrigin(req *http.Request, via []*http.Request) error {
	first := via[0].URL
	if len(via) > 3 || req.URL.Scheme != first.Scheme || !strings.EqualFold(req.URL.Host, first.Host) {
		return http.ErrUseLastResponse
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.RegisterKey != "" && method == http.MethodPost {
		req.Header.Set("X-Register-Key", c.RegisterKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		return &StatusError{Code: resp.StatusCode, Msg: e.Error}
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
	}
	return nil
}

// StatusError is a non-2xx directory reply.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return fmt.Sprintf("directory: %d %s", e.Code, e.Msg) }

// Register lists a server and returns its ID and update token.
func (c *Client) Register(ctx context.Context, req api.RegisterRequest) (api.RegisterResponse, error) {
	var out api.RegisterResponse
	err := c.do(ctx, http.MethodPost, "/v1/servers", "", req, &out)
	return out, err
}

// Heartbeat refreshes a listing.
func (c *Client) Heartbeat(ctx context.Context, id, token string, hb api.Heartbeat) error {
	return c.do(ctx, http.MethodPut, "/v1/servers/"+url.PathEscape(id), token, hb, nil)
}

// Unregister removes a listing.
func (c *Client) Unregister(ctx context.Context, id, token string) error {
	return c.do(ctx, http.MethodDelete, "/v1/servers/"+url.PathEscape(id), token, nil, nil)
}

// Self returns the host's own listing, including whether players see it yet
// (Listed).
func (c *Client) Self(ctx context.Context, id, token string) (api.ServerInfo, error) {
	var out api.ServerInfo
	err := c.do(ctx, http.MethodGet, "/v1/servers/"+url.PathEscape(id), token, nil, &out)
	return out, err
}

// List returns listings, optionally only those of one build.
func (c *Client) List(ctx context.Context, build string) ([]api.ServerInfo, error) {
	var out []api.ServerInfo
	path := "/v1/servers"
	if build != "" {
		path += "?build=" + url.QueryEscape(build)
	}
	err := c.do(ctx, http.MethodGet, path, "", nil, &out)
	return out, err
}

// Beacon fetches a server's latest beacon.
func (c *Client) Beacon(ctx context.Context, id string) (api.BeaconResponse, error) {
	var out api.BeaconResponse
	err := c.do(ctx, http.MethodGet, "/v1/servers/"+url.PathEscape(id)+"/beacon", "", nil, &out)
	return out, err
}
