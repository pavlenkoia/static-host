package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"static-host/internal/publication"
)

type domainListInput struct {
	SiteID string `json:"site_id"`
}

type domainAddInput struct {
	SiteID   string `json:"site_id"`
	Hostname string `json:"hostname"`
}

type domainMutationInput struct {
	SiteID   string `json:"site_id"`
	DomainID string `json:"domain_id"`
}

func registerDomainTools(server *mcp.Server, s *publication.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "domains_list",
		Description: "List custom domains for a site.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainListInput) (*mcp.CallToolResult, any, error) {
		domains, err := s.ListDomains(ctx, toolIdentity(ctx).OwnerID, in.SiteID)
		if err != nil {
			return toolError(err)
		}
		return nil, map[string]any{"domains": domains}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "domains_add",
		Description: "Request a custom domain for a site.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainAddInput) (*mcp.CallToolResult, any, error) {
		domain, err := s.AddDomain(ctx, toolIdentity(ctx).OwnerID, in.SiteID, in.Hostname)
		if err != nil {
			return toolError(err)
		}
		return nil, map[string]any{"domain": domain}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "domains_check",
		Description: "Schedule a DNS and HTTPS check for a custom domain.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainMutationInput) (*mcp.CallToolResult, any, error) {
		domain, err := s.CheckDomain(ctx, toolIdentity(ctx).OwnerID, in.SiteID, in.DomainID)
		if err != nil {
			return toolError(err)
		}
		return nil, map[string]any{"domain": domain}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "domains_delete",
		Description: "Remove a custom domain from a site.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in domainMutationInput) (*mcp.CallToolResult, any, error) {
		if err := s.DeleteDomain(ctx, toolIdentity(ctx).OwnerID, in.SiteID, in.DomainID); err != nil {
			return toolError(err)
		}
		return nil, map[string]any{"deleted": true, "site_id": in.SiteID, "domain_id": in.DomainID}, nil
	})
}
