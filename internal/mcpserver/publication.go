package mcpserver

import (
 "context"
 "errors"

 "github.com/modelcontextprotocol/go-sdk/mcp"
 "static-host/internal/publication"
)

type siteInput struct { SiteID string `json:"site_id"` }
type createSiteInput struct { Name string `json:"name"` }
type uploadInput struct { SiteID string `json:"site_id"`; Format string `json:"format"`; ExpectedGeneration *int64 `json:"expected_generation"` }
type publishInput struct { SiteID string `json:"site_id"`; UploadID string `json:"upload_id"`; ExpectedGeneration *int64 `json:"expected_generation"`; IdempotencyKey string `json:"idempotency_key"` }
func toolIdentity(ctx context.Context) publication.Identity {id,_:=ctx.Value(identityKey{}).(Identity); return publication.Identity{OwnerID:id.OwnerID,KeyID:id.KeyID}}
func toolError(err error) (*mcp.CallToolResult,any,error) {code:="service_unavailable"; var e *publication.Error; if errors.As(err,&e) {code=e.Code}; return &mcp.CallToolResult{IsError:true},map[string]any{"error":map[string]string{"code":code,"message":code}},nil}
func registerPublicationTools(server *mcp.Server,s *publication.Service) {
 mcp.AddTool(server,&mcp.Tool{Name:"sites_create",Description:"Create a draft site. Next call uploads_create, PUT the ZIP or tar.gz with the same MCP bearer key, then sites_publish. Archives require a nonempty root index.html."},func(ctx context.Context,_ *mcp.CallToolRequest,in createSiteInput)(*mcp.CallToolResult,any,error){site,err:=s.Create(ctx,toolIdentity(ctx).OwnerID,in.Name); if err!=nil {return toolError(err)}; return nil,map[string]any{"site":site},nil})
 mcp.AddTool(server,&mcp.Tool{Name:"sites_list",Description:"List the owner's sites."},func(ctx context.Context,_ *mcp.CallToolRequest,_ struct{})(*mcp.CallToolResult,any,error){sites,err:=s.List(ctx,toolIdentity(ctx).OwnerID); if err!=nil {return toolError(err)}; return nil,map[string]any{"sites":sites},nil})
 mcp.AddTool(server,&mcp.Tool{Name:"sites_get",Description:"Get a site and its current generation."},func(ctx context.Context,_ *mcp.CallToolRequest,in siteInput)(*mcp.CallToolResult,any,error){site,err:=s.Get(ctx,toolIdentity(ctx).OwnerID,in.SiteID); if err!=nil {return toolError(err)}; return nil,map[string]any{"site":site},nil})
 mcp.AddTool(server,&mcp.Tool{Name:"sites_delete",Description:"Permanently delete a site and its contents; this cannot be undone."},func(ctx context.Context,_ *mcp.CallToolRequest,in siteInput)(*mcp.CallToolResult,any,error){if err:=s.Delete(ctx,toolIdentity(ctx).OwnerID,in.SiteID); err!=nil {return toolError(err)}; return nil,map[string]any{"deleted":true,"site_id":in.SiteID},nil})
 mcp.AddTool(server,&mcp.Tool{Name:"uploads_create",Description:"Create one 15-minute upload grant for zip or tar.gz. PUT the complete archive outside MCP using the same bearer key. Maximum 100 MiB archive, 250 MiB expanded and 10000 files; root index.html is required."},func(ctx context.Context,_ *mcp.CallToolRequest,in uploadInput)(*mcp.CallToolResult,any,error){if in.ExpectedGeneration==nil {return toolError(&publication.Error{Code:"invalid_request"})}; grant,err:=s.CreateUpload(ctx,toolIdentity(ctx),in.SiteID,in.Format,*in.ExpectedGeneration); if err!=nil {return toolError(err)}; return nil,grant,nil})
 mcp.AddTool(server,&mcp.Tool{Name:"sites_publish",Description:"Publish a ready upload as a complete replacement of the previous snapshot. Supply the expected generation and a unique idempotency key."},func(ctx context.Context,_ *mcp.CallToolRequest,in publishInput)(*mcp.CallToolResult,any,error){if in.ExpectedGeneration==nil {return toolError(&publication.Error{Code:"invalid_request"})}; site,err:=s.Publish(ctx,toolIdentity(ctx),in.SiteID,in.UploadID,*in.ExpectedGeneration,in.IdempotencyKey); if err!=nil {return toolError(err)}; return nil,map[string]any{"site":site},nil})
}
