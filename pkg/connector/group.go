package connector

import (
	"context"
	"errors"
	"fmt"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"net/url"
	"path"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
)

type groupBuilder struct {
	conn   *Connector
	client *client.AzureClient
}

const (
	odataTypeGroup            = "#microsoft.graph.group"
	odataTypeUser             = "#microsoft.graph.user"
	odataTypeServicePrincipal = "#microsoft.graph.servicePrincipal"
	odataTypeDevice           = "#microsoft.graph.device"
	spTypeApplication         = "Application"
	spTypeManagedIdentity     = "ManagedIdentity"
	spTypeLegacy              = "Legacy"
	spTypeSocialIdp           = "SocialIdp"
	typeOwners                = "owners"
	typeMembers               = "members"
	typeAssigned              = "assigned"
)

func (g *groupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return groupResourceType
}

func (g *groupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	resp, err := g.client.Groups(ctx, pToken.Token)
	if err != nil {
		return nil, "", nil, err
	}

	groups, err := ConvertErr(resp.Groups, func(g *client.Group) (*v2.Resource, error) {
		return groupResource(ctx, g, parentResourceID)
	})
	if err != nil {
		return nil, "", nil, err
	}

	return groups, resp.NextLink, nil, nil
}

// Entitlements always returns an empty slice for users.
func (g *groupBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	options := []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Group Owner", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Owner of %s group", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType),
	}
	rv = append(rv, ent.NewPermissionEntitlement(resource, typeOwners, options...))

	options = []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Group Member", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Member of %s group", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType, groupResourceType),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, typeMembers, options...))

	return rv, "", nil, nil
}

func (g *groupBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	b := &pagination.Bag{}
	err := b.Unmarshal(pToken.Token)
	if err != nil {
		return nil, "", nil, err
	}

	// NOTE: We use the Beta URL here because in the v1.0 docs there is this note (last checked December 2024)
	// -----------------------------------------------------------------------------------------------
	// *** Important ***
	//
	//   This API has a known issue where service principals are not listed as group
	//   members in v1.0. Use this API on the beta endpoint instead or the
	//   /groups/{id}?members API.
	//
	// https://learn.microsoft.com/en-us/graph/api/group-list-members?view=graph-rest-1.0&tabs=http
	//
	// NOTE #2: This applies to both the members and owners endpoints.
	if b.Current() == nil {
		b.Push(pagination.PageState{
			ResourceTypeID: typeOwners,
			Token:          "",
		})

		b.Push(pagination.PageState{
			ResourceTypeID: typeMembers,
			Token:          "",
		})
	}

	ps := b.Pop()
	if ps == nil {
		return nil, "", nil, nil
	}

	groupId := resource.Id.Resource
	var memberShip *client.MembershipList

	switch ps.ResourceTypeID {
	case typeOwners:
		memberShip, err = g.client.GroupOwners(ctx, groupId)
	case typeMembers:
		memberShip, err = g.client.GroupMembers(ctx, groupId, ps.Token)
	default:
		return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: unknown resource type ID %s", ps.ResourceTypeID)
	}

	if err != nil {
		if status.Code(err) == codes.NotFound {
			l.Warn(
				"group membership not found",
				zap.String("type", ps.ResourceTypeID),
				zap.String("group_id", groupId),
				zap.Error(err),
			)
			return nil, "", nil, nil
		}

		return nil, "", nil, err
	}

	if memberShip.NextLink != "" {
		b.Push(pagination.PageState{
			ResourceTypeID: ps.ResourceTypeID,
			ResourceID:     ps.ResourceID,
			Token:          memberShip.NextLink,
		})
	}

	grants, err := getGroupGrants(ctx, memberShip, resource, g, ps)
	if err != nil {
		return nil, "", nil, err
	}

	nextToken, err := b.Marshal()
	if err != nil {
		return nil, "", nil, err
	}

	return grants, nextToken, nil, nil
}

func (g *groupBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	if principal.Id.ResourceType != userResourceType.Id {
		l.Warn(
			"baton-azure-infrastructure: only users can be granted group membership",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)

		return nil, errors.New("baton-azure-infrastructure: only users can be granted group entitlements")
	}

	var reqURL string
	groupID := entitlement.Resource.Id.Resource
	switch {
	case strings.HasSuffix(entitlement.Id, ":owners"):
		// https://learn.microsoft.com/en-us/graph/api/group-post-owners?view=graph-rest-1.0&tabs=http
		reqURL = g.conn.buildURL(path.Join("groups", groupID, "owners", "$ref"), url.Values{})
	case strings.HasSuffix(entitlement.Id, ":members"):
		// https://learn.microsoft.com/en-us/graph/api/group-post-members?view=graph-rest-1.0&tabs=http
		reqURL = g.conn.buildURL(path.Join("groups", groupID, "members", "$ref"), url.Values{})
	default:
		return nil, errors.New("baton-azure-infrastructure: only members can provision membership or owners entitlements to a group")
	}

	objRef := getGroupGrantURL(principal)
	assign := &assignment{
		ObjectRef: objRef,
	}
	body, err := assign.MarshalToReader()
	if err != nil {
		return nil, err
	}

	err = g.conn.query(ctx, graphReadScopes, http.MethodPost, reqURL, body, nil)
	if err != nil {
		if strings.Contains(err.Error(), "added object references already exist") {
			l.Info("Attempted to grant a group membership that already exists, treating as successful")
			return nil, nil
		}

		return nil, err
	}

	return nil, nil
}

func (g *groupBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	entitlement := grant.Entitlement
	principal := grant.Principal
	if principal.Id.ResourceType != userResourceType.Id {
		l.Warn(
			"baton-azure-infrastructure: only users can be granted group membership",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, errors.New("baton-azure-infrastructure: only users can be granted group entitlements")
	}

	var reqURL string
	groupID := entitlement.Resource.Id.Resource
	userID := principal.Id.Resource
	switch {
	case strings.HasSuffix(entitlement.Id, ":owners"):
		// https://learn.microsoft.com/en-us/graph/api/group-post-owners?view=graph-rest-1.0&tabs=http
		reqURL = g.conn.buildURL(path.Join("groups", groupID, "owners", userID, "$ref"), url.Values{})
	case strings.HasSuffix(entitlement.Id, ":members"):
		// https://learn.microsoft.com/en-us/graph/api/group-delete-members?view=graph-rest-1.0&tabs=http
		reqURL = g.conn.buildURL(path.Join("groups", groupID, "members", userID, "$ref"), url.Values{})
	default:
		return nil, errors.New("baton-azure-infrastructure: only can revoke membership or owners entitlements to a group")
	}

	err := g.conn.query(ctx, graphReadScopes, http.MethodDelete, reqURL, nil, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			l.Info("Group membership to revoke not found; treating as successful because the end state is achieved")
			return nil, nil
		}

		return nil, err
	}

	return nil, nil
}

func newGroupBuilder(c *Connector) *groupBuilder {
	return &groupBuilder{
		conn:   c,
		client: c.client,
	}
}
