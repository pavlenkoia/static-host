package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDomainMode(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "server")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "STATIC_HOST_LISTEN=") &&
			!strings.HasPrefix(v, "STATIC_HOST_DATA_DIR=") &&
			!strings.HasPrefix(v, "STATIC_HOST_BASE_DOMAIN=") &&
			!strings.HasPrefix(v, "STATIC_HOST_ADMIN_PASSWORD=") &&
			!strings.HasPrefix(v, "STATIC_HOST_DOMAIN_INGRESS_IPS=") &&
			!strings.HasPrefix(v, "STATIC_HOST_DOMAIN_RESERVED_HOSTS=") {
			env = append(env, v)
		}
	}
	env = append(env, "STATIC_HOST_DATA_DIR="+dir, "STATIC_HOST_ADMIN_PASSWORD=long-password-one")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	askAddr := "127.0.0.1:8082"
	cmd := exec.Command(bin)
	cmd.Env = append(env, "STATIC_HOST_LISTEN="+addr, "STATIC_HOST_BASE_DOMAIN=example.test", "STATIC_HOST_DOMAIN_INGRESS_IPS=1.1.1.1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	client := &http.Client{Timeout: time.Second}
	url := "http://" + addr
	ready := false
	for i := 0; i < 100; i++ {
		req, e := http.NewRequest(http.MethodGet, url+"/healthz", nil)
		if e == nil {
			req.Host = "panel.example.test"
			resp, e := client.Do(req)
			if e == nil {
				resp.Body.Close()
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("not ready: %s", stderr.String())
	}
	request := func(host, method, path, origin, body, csrf string, cookie *http.Cookie, token string) (int, []byte, *http.Response) {
		r, e := http.NewRequest(method, url+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Accept", "application/json, text/event-stream")
		}
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, b, resp
	}
	askResponse, err := client.Get("http://" + askAddr + "/ask?domain=unknown.example.org")
	if err != nil {
		t.Fatal(err)
	}
	askResponse.Body.Close()
	if askResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("ask listener returned %d for an unclaimed domain", askResponse.StatusCode)
	}
	publicStatus, publicBody, _ := request("unknown.example.org", http.MethodGet, "/ask?domain=unknown.example.org", "", "", "", nil, "")
	if publicStatus != http.StatusNotFound {
		t.Fatalf("public ask route returned %d %s", publicStatus, publicBody)
	}
	status, body, resp := request("panel.example.test", "POST", "/api/login", "https://panel.example.test", `{"login":"admin","password":"long-password-one"}`, "", nil, "")
	if status != 200 {
		t.Fatalf("login %d %s", status, body)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-static_host_session" || !cookies[0].Secure {
		t.Fatal(cookies)
	}
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if err = json.Unmarshal(body, &session); err != nil {
		t.Fatal(err)
	}
	status, body, _ = request("panel.example.test", "GET", "/api/session", "", "", "", cookies[0], "")
	if status != 200 || !bytes.Contains(body, []byte("admin")) {
		t.Fatalf("session %d %s", status, body)
	}
	status, body, _ = request("panel.example.test", "POST", "/api/keys", "https://panel.example.test", `{"name":"agent"}`, session.CSRF, cookies[0], "")
	if status != 201 {
		t.Fatalf("key %d %s", status, body)
	}
	var key struct {
		Token string `json:"token"`
	}
	if err = json.Unmarshal(body, &key); err != nil || key.Token == "" {
		t.Fatalf("token %v %s", err, body)
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	status, body, _ = request("mcp.example.test", "POST", "/mcp", "", initialize, "", nil, key.Token)
	if status != 200 {
		t.Fatalf("mcp %d %s", status, body)
	}
	for _, x := range []struct{ host, path string }{{"panel.example.test", "/mcp"}, {"mcp.example.test", "/api/session"}} {
		status, _, _ = request(x.host, "GET", x.path, "", "", "", nil, "")
		if status != 404 {
			t.Fatal(fmt.Sprintf("%s %s: %d", x.host, x.path, status))
		}
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.Wait() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("server shutdown: %v", err)
		}
	case <-time.After(6 * time.Second):
		_ = cmd.Process.Kill()
		<-stopped
		t.Fatal("server did not stop")
	}
	cmd.Process = nil
	for _, endpoint := range []string{url + "/healthz", "http://" + askAddr + "/ask?domain=unknown.example.org"} {
		resp, err := client.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			t.Fatalf("listener remained available after shutdown: %s", endpoint)
		}
	}
}
