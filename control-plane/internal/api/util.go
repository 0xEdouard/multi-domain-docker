package api

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xEdouard/multi-domain-infra/control-plane/internal/models"
)

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

func renderTraefikConfig(services []*models.Service, resolver string) string {
	if resolver == "" {
		resolver = "le"
	}

	var routerBlocks []string
	var serviceBlocks []string

	for _, svc := range services {
		if svc == nil {
			continue
		}
		serviceKey := sanitizeKey(svc.Name)
		if serviceKey == "" {
			serviceKey = sanitizeKey(svc.ID)
		}
		port := svc.InternalPort
		if port == 0 {
			port = 80
		}

		serviceBlocks = append(serviceBlocks, renderServiceBlock(serviceKey, port))

		for _, domain := range svc.Domains {
			routerName := serviceKey + "-" + sanitizeKey(domain.Environment) + "-" + sanitizeKey(domain.Hostname)
			if routerName == "" {
				routerName = serviceKey + "-" + newID()
			}
			routerBlocks = append(routerBlocks, renderRouterBlock(routerName, domain.Hostname, serviceKey, resolver))
		}
	}

	sort.Strings(routerBlocks)
	sort.Strings(serviceBlocks)

	var builder strings.Builder
	builder.WriteString("http:\n")
	builder.WriteString("  routers:\n")
	if len(routerBlocks) == 0 {
		builder.WriteString("    {}\n")
	} else {
		for _, block := range routerBlocks {
			builder.WriteString(block)
		}
	}
	builder.WriteString("  services:\n")
	if len(serviceBlocks) == 0 {
		builder.WriteString("    {}\n")
	} else {
		for _, block := range serviceBlocks {
			builder.WriteString(block)
		}
	}

	return builder.String()
}

func sanitizeKey(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func renderRouterBlock(name, hostname, serviceKey, resolver string) string {
	var builder strings.Builder
	builder.WriteString("    ")
	builder.WriteString(name)
	builder.WriteString(":\n")
	builder.WriteString("      rule: Host(`")
	builder.WriteString(hostname)
	builder.WriteString("`)\n")
	builder.WriteString("      service: ")
	builder.WriteString(serviceKey)
	builder.WriteString("\n")
	builder.WriteString("      entryPoints:\n")
	builder.WriteString("        - websecure\n")
	builder.WriteString("      tls:\n")
	builder.WriteString("        certResolver: ")
	builder.WriteString(resolver)
	builder.WriteString("\n")
	return builder.String()
}

func renderServiceBlock(name string, port int) string {
	var builder strings.Builder
	builder.WriteString("    ")
	builder.WriteString(name)
	builder.WriteString(":\n")
	builder.WriteString("      loadBalancer:\n")
	builder.WriteString("        servers:\n")
	builder.WriteString("          - url: http://127.0.0.1:")
	builder.WriteString(strconv.Itoa(port))
	builder.WriteString("\n")
	return builder.String()
}
