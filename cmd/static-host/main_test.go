package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCLIHTTPAndLifecycle(t *testing.T) {
	for _, key := range []string{"STATIC_HOST_LISTEN", "STATIC_HOST_DATA_DIR", "STATIC_HOST_BASE_DOMAIN", "STATIC_HOST_ADMIN_PASSWORD", "STATIC_HOST_DOMAIN_INGRESS_IPS", "STATIC_HOST_DOMAIN_RESERVED_HOSTS", "STATIC_HOST_DOMAIN_ASK_LISTEN"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	temp := t.TempDir()
	binary := filepath.Join(temp, "static-host")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	dataDir := filepath.Join(temp, "data")
	if err := os.Mkdir(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	uninitialized := exec.Command(binary, "-data-dir", dataDir, "-listen", "127.0.0.1:8081")
	if output, err := uninitialized.CombinedOutput(); err == nil || !strings.Contains(string(output), "STATIC_HOST_ADMIN_PASSWORD") {
		t.Fatalf("missing admin password: err=%v output=%s", err, output)
	}
	for _, port := range []string{"0", "65536"} {
		cmd := exec.Command(binary, "-data-dir", dataDir, "-listen", "127.0.0.1:"+port)
		cmd.Env = append(os.Environ(), "STATIC_HOST_ADMIN_PASSWORD=valid-password-12")
		if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "invalid port") {
			t.Fatalf("invalid port %s: err=%v output=%s", port, err, output)
		}
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	cmd := exec.Command(binary, "-data-dir", dataDir, "-listen", addr)
	cmd.Env = append(os.Environ(), "STATIC_HOST_ADMIN_PASSWORD=long-password-one")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	client := &http.Client{Timeout: time.Second}
	healthURL := "http://" + addr + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	var response *http.Response
	for time.Now().Before(deadline) {
		response, err = client.Get(healthURL)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cmd.Process = nil
		out, _ := io.ReadAll(stdout)
		errOut, _ := io.ReadAll(stderr)
		t.Fatalf("server did not start: %v\nstdout=%s\nstderr=%s", err, out, errOut)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || string(body) != "ok\n" || response.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("GET /healthz: status=%d content-type=%q body=%q err=%v", response.StatusCode, response.Header.Get("Content-Type"), body, readErr)
	}
	head, err := client.Head(healthURL)
	if err != nil {
		t.Fatal(err)
	}
	headBody, readErr := io.ReadAll(head.Body)
	_ = head.Body.Close()
	if readErr != nil || head.StatusCode != http.StatusOK || len(headBody) != 0 || head.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("HEAD /healthz: status=%d content-type=%q body=%q err=%v", head.StatusCode, head.Header.Get("Content-Type"), headBody, readErr)
	}
	request := func(method, path, body string, cookie *http.Cookie, csrf, token string) (int, []byte, []*http.Cookie) {
		req, err := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/json, text/event-stream")
		}
		req.Header.Set("Origin", "http://"+addr)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, bodyBytes, resp.Cookies()
	}
	status, body, cookies := request("POST", "/api/login", `{"login":"admin","password":"long-password-one"}`, nil, "", "")
	if status != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("login: %d %s", status, body)
	}
	sessionCookie := cookies[0]
	var loginResult struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(body, &loginResult); err != nil || loginResult.CSRF == "" {
		t.Fatalf("login response: %v %s", err, body)
	}
	status, body, _ = request("POST", "/api/keys", `{"name":"persisted"}`, sessionCookie, loginResult.CSRF, "")
	if status != http.StatusCreated {
		t.Fatalf("create MCP key: %d %s", status, body)
	}
	var keyResult struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &keyResult); err != nil || keyResult.Token == "" {
		t.Fatalf("key response: %v %s", err, body)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server did not exit cleanly after SIGTERM-equivalent signal: %v", err)
		}
	case <-time.After(6 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("server did not shut down after SIGTERM-equivalent signal")
	}
	cmd.Process = nil
	if response, err := client.Get(healthURL); err == nil {
		_ = response.Body.Close()
		t.Fatalf("health endpoint remained available after shutdown on %s", addr)
	}
	restarted := exec.Command(binary, "-data-dir", dataDir, "-listen", addr)
	restarted.Env = append(os.Environ(), "STATIC_HOST_ADMIN_PASSWORD=rotated-admin-password")
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if restarted.Process != nil {
			_ = restarted.Process.Kill()
			_ = restarted.Wait()
		}
	}()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err = client.Get(healthURL)
		if err == nil {
			response.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server did not restart: %v", err)
	}
	status, body, _ = request("GET", "/api/session", "", sessionCookie, "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("old session survived restart: %d %s", status, body)
	}
	status, body, _ = request("POST", "/api/login", `{"login":"admin","password":"long-password-one"}`, nil, "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("old password survived rotation: %d %s", status, body)
	}
	status, body, _ = request("POST", "/api/login", `{"login":"admin","password":"rotated-admin-password"}`, nil, "", "")
	if status != http.StatusOK {
		t.Fatalf("rotated password rejected: %d %s", status, body)
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	status, body, _ = request("POST", "/mcp", initialize, nil, "", keyResult.Token)
	if status != http.StatusOK {
		t.Fatalf("MCP key lost after restart: %d %s", status, body)
	}
}
