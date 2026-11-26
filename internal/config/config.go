package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"static-host/internal/domainname"
)

type Config struct {
	Listen, DataDir, BaseDomain, PanelOrigin, MCPOrigin, UploadOrigin, SiteSuffix, AdminPassword string
	DomainIngressIPs                                                                             []netip.Addr
	DomainReservedHosts                                                                          []string
	DomainAskListen                                                                              string
}

func value(env func(string) (string, bool), key, fallback string) string {
	if v, ok := env(key); ok {
		return v
	}
	return fallback
}
func Parse(args []string, env func(string) (string, bool)) (Config, error) {
	var c Config
	var localOrigin, ingressIPs, reservedHosts string
	f := flag.NewFlagSet("static-host", flag.ContinueOnError)
	f.StringVar(&c.Listen, "listen", value(env, "STATIC_HOST_LISTEN", "127.0.0.1:8081"), "HTTP listen address")
	f.StringVar(&c.DataDir, "data-dir", value(env, "STATIC_HOST_DATA_DIR", "./data"), "data directory")
	f.StringVar(&c.BaseDomain, "base-domain", value(env, "STATIC_HOST_BASE_DOMAIN", ""), "public base domain")
	f.StringVar(&localOrigin, "local-origin", value(env, "STATIC_HOST_LOCAL_ORIGIN", ""), "explicit HTTP origin for local mode")
	f.StringVar(&ingressIPs, "domain-ingress-ips", value(env, "STATIC_HOST_DOMAIN_INGRESS_IPS", ""), "comma-separated custom-domain ingress IPs")
	f.StringVar(&reservedHosts, "domain-reserved-hosts", value(env, "STATIC_HOST_DOMAIN_RESERVED_HOSTS", ""), "comma-separated reserved hostnames")
	f.StringVar(&c.DomainAskListen, "domain-ask-listen", value(env, "STATIC_HOST_DOMAIN_ASK_LISTEN", ""), "numeric IP and port for the internal domain ask listener")
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
	localMode := c.BaseDomain == ""
	if err := parseDomainSettings(&c, ingressIPs, reservedHosts, localMode); err != nil {
		return c, err
	}
	if localMode {
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

func parseDomainSettings(c *Config, ingressList, reservedList string, localMode bool) error {
	if strings.TrimSpace(ingressList) != "" {
		for _, raw := range strings.Split(ingressList, ",") {
			raw = strings.TrimSpace(raw)
			ip, err := netip.ParseAddr(raw)
			if err != nil || ip.Zone() != "" {
				return fmt.Errorf("invalid domain ingress IP %q", raw)
			}
			ip = ip.Unmap()
			localOnlyUnicast := localMode && (ip.IsLoopback() || ip.IsPrivate())
			if (!ip.IsGlobalUnicast() && !localOnlyUnicast) || ip.IsLinkLocalUnicast() ||
				(!localMode && (ip.IsPrivate() || ip.IsLoopback())) {
				return fmt.Errorf("domain ingress IP %q is not allowed", raw)
			}
			for _, existing := range c.DomainIngressIPs {
				if existing == ip {
					return fmt.Errorf("duplicate domain ingress IP %q", raw)
				}
			}
			c.DomainIngressIPs = append(c.DomainIngressIPs, ip)
		}
	}
	if strings.TrimSpace(reservedList) != "" {
		for _, raw := range strings.Split(reservedList, ",") {
			hostname, err := domainname.Normalize(raw)
			if err != nil {
				return fmt.Errorf("invalid reserved domain %q", strings.TrimSpace(raw))
			}
			for _, existing := range c.DomainReservedHosts {
				if existing == hostname {
					return fmt.Errorf("duplicate reserved domain %q", hostname)
				}
			}
			c.DomainReservedHosts = append(c.DomainReservedHosts, hostname)
		}
	}
	if len(c.DomainIngressIPs) == 0 {
		if c.DomainAskListen != "" {
			return errors.New("STATIC_HOST_DOMAIN_ASK_LISTEN requires domain ingress IPs")
		}
		return nil
	}
	if c.DomainAskListen == "" {
		return errors.New("domain ingress IPs require STATIC_HOST_DOMAIN_ASK_LISTEN")
	}
	askHost, askPort, err := net.SplitHostPort(c.DomainAskListen)
	if err != nil || askPort == "" {
		return errors.New("invalid domain ask listen address")
	}
	askIP, err := netip.ParseAddr(askHost)
	if err != nil {
		return errors.New("invalid domain ask listen address")
	}
	if askIP.Zone() != "" {
		return errors.New("invalid domain ask listen address")
	}
	for _, ch := range askPort {
		if ch < '0' || ch > '9' {
			return errors.New("invalid domain ask listen address")
		}
	}
	n, err := strconv.Atoi(askPort)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("invalid domain ask listen address")
	}
	return nil
}
