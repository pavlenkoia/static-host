package main

import (
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
	for _, key := range []string{"STATIC_HOST_LISTEN", "STATIC_HOST_DATA_DIR", "STATIC_HOST_BASE_DOMAIN"} { t.Setenv(key, ""); os.Unsetenv(key) }
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
	if output, err := uninitialized.CombinedOutput(); err == nil || !strings.Contains(string(output), "owner not initialized") {
		t.Fatalf("uninitialized owner: err=%v output=%s", err, output)
	}
	for _, port := range []string{"0", "65536"} {
		cmd := exec.Command(binary, "-data-dir", dataDir, "-listen", "127.0.0.1:"+port)
		if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "invalid port") {
			t.Fatalf("invalid port %s: err=%v output=%s", port, err, output)
		}
	}

	initOwner := exec.Command(binary, "owner", "init", "-data-dir", dataDir, "-login", "admin")
	initOwner.Stdin = strings.NewReader("long-password-one\n")
	if output, err := initOwner.CombinedOutput(); err != nil {
		t.Fatalf("initialize owner: %v\n%s", err, output)
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
}
