package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Config struct{ Listen, DataDir, BaseDomain, PanelOrigin, MCPOrigin, UploadOrigin, SiteSuffix, AdminPassword string }

func value(env func(string) (string, bool), key, fallback string) string {
	if v, ok := env(key); ok {
		return v
	}
	return fallback
}
func Parse(args []string, env func(string) (string, bool)) (Config, error) {
	var c Config
	var localOrigin string
	f := flag.NewFlagSet("static-host", flag.ContinueOnError)
	f.StringVar(&c.Listen, "listen", value(env, "STATIC_HOST_LISTEN", "127.0.0.1:8081"), "HTTP listen address")
	f.StringVar(&c.DataDir, "data-dir", value(env, "STATIC_HOST_DATA_DIR", "./data"), "data directory")
	f.StringVar(&c.BaseDomain, "base-domain", value(env, "STATIC_HOST_BASE_DOMAIN", ""), "public base domain")
	f.StringVar(&localOrigin, "local-origin", value(env, "STATIC_HOST_LOCAL_ORIGIN", ""), "explicit HTTP origin for local mode")
	if err := f.Parse(args); err != nil {
		return c, err
	}
	if f.NArg() != 0 {
		return c, errors.New("unexpected arguments")
	}
	if c.DataDir == "" {
		return c, errors.New("empty data directory")
	}
	c.AdminPassword, _ = env("STATIC_HOST_ADMIN_PASSWORD")
	if len(c.AdminPassword) < 12 || len(c.AdminPassword) > 72 {
		return c, errors.New("STATIC_HOST_ADMIN_PASSWORD must be 12–72 bytes")
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return c, fmt.Errorf("invalid listen: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return c, errors.New("listen host must be a numeric IP")
	}
	if port == "" {
		return c, errors.New("invalid port")
	}
	for _, ch := range port {
		if ch < '0' || ch > '9' {
			return c, errors.New("invalid port")
		}
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return c, errors.New("invalid port")
	}
	for _, ch := range c.BaseDomain {
		if ch > 127 {
			return c, errors.New("invalid base domain")
		}
	}
	c.BaseDomain = strings.ToLower(c.BaseDomain)
	if c.BaseDomain == "" {
		if !ip.IsLoopback() && !ip.IsUnspecified() {
			return c, errors.New("local mode requires loopback or wildcard IP")
		}
		originHost := host
		if ip.IsUnspecified() {
			if ip.To4() != nil {
				originHost = "127.0.0.1"
			} else {
				originHost = "::1"
			}
		}
		c.PanelOrigin = "http://" + net.JoinHostPort(originHost, port)
		if localOrigin != "" {
			u, e := url.Parse(localOrigin)
			if e != nil || u.Scheme != "http" || u.User != nil || localOrigin != "http://"+u.Host || u.Port() != port {
				return c, errors.New("invalid local origin")
			}
			originIP := net.ParseIP(u.Hostname())
			if originIP == nil || originIP.IsUnspecified() || u.Host != net.JoinHostPort(u.Hostname(), port) {
				return c, errors.New("invalid local origin")
			}
			c.PanelOrigin = localOrigin
		}
		c.MCPOrigin = c.PanelOrigin
		c.UploadOrigin = c.PanelOrigin
		c.SiteSuffix = net.JoinHostPort("localhost", port)
		return c, nil
	}
	if localOrigin != "" {
		return c, errors.New("local origin requires local mode")
	}
	if len(c.BaseDomain)+len("s-")+32+len(".sites.") > 253 || net.ParseIP(c.BaseDomain) != nil {
		return c, errors.New("invalid base domain")
	}
	labels := strings.Split(c.BaseDomain, ".")
	if len(labels) < 2 {
		return c, errors.New("invalid base domain")
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return c, errors.New("invalid base domain")
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return c, errors.New("invalid base domain")
			}
		}
	}
	c.PanelOrigin = "https://panel." + c.BaseDomain
	c.MCPOrigin = "https://mcp." + c.BaseDomain
	c.UploadOrigin = "https://upload." + c.BaseDomain
	c.SiteSuffix = "sites." + c.BaseDomain
	return c, nil
}
