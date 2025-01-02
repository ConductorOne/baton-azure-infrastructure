package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/internal/slices"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
)

type groupBuilder struct {
	conn                       *Connector
	knownGroupMembershipTypes  map[string]bool
	knownServicePrincipalTypes map[string]bool
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
)

func (g *groupBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return groupResourceType
}

func (g *groupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, err := parsePageToken(pToken.Token, &v2.ResourceId{ResourceType: groupResourceType.Id})
	if err != nil {
		return nil, "", nil, err
	}

	reqURL := bag.PageToken()
	if reqURL == "" {
		v := setGroupKeys()
		if g.conn.SkipAdGroups {
			v.Set("$filter", "(onPremisesSyncEnabled ne true)")
			v.Set("$count", "true") // Required to prevent MS Graph from returning a 400
		}

		reqURL = g.conn.buildURL("groups", v)
	}

	resp := &groupsList{}
	err = g.conn.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, "", nil, err
	}

	groups, err := slices.ConvertErr(resp.Groups, func(g *group) (*v2.Resource, error) {
		return groupResource(ctx, g, parentResourceID)
	})
	if err != nil {
		return nil, "", nil, err
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	return groups, pageToken, nil, nil
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
	b := &pagination.Bag{}
	err := b.Unmarshal(pToken.Token)
	if err != nil {
		return nil, "", nil, err
	}

	// NOTE: We use the Beta URL here because in the v1.0 docs there is this note (last checked August 2023)
	//
	// Important
	//
	//   This API has a known issue where service principals are not listed as group
	//   members in v1.0. Use this API on the beta endpoint instead or the
	//   /groups/{id}?members API.
	//
	// https://learn.microsoft.com/en-us/graph/api/group-list-members?view=graph-rest-1.0&tabs=http
	//
	// NOTE #2: This applies to both the members and owners endpoints.
	if b.Current() == nil {
		owenrsQuery := url.Values{}
		owenrsQuery.Set("$select", strings.Join([]string{"id"}, ","))
		ownersURL := g.conn.buildBetaURL(path.Join("groups", resource.Id.Resource, "owners"), owenrsQuery)
		b.Push(pagination.PageState{
			ResourceTypeID: typeOwners,
			Token:          ownersURL,
		})

		memberQuery := setMemberQuery()
		if g.conn.SkipAdGroups {
			memberQuery.Set("$filter", "(onPremisesSyncEnabled ne true)")
			memberQuery.Set("$count", "true") // Required to prevent MS Graph from returning a 400
		}

		membersURL := g.conn.buildBetaURL(path.Join("groups", resource.Id.Resource, "members"), memberQuery)
		b.Push(pagination.PageState{
			ResourceTypeID: typeMembers,
			Token:          membersURL,
		})
	}

	ps := b.Current()
	resp := &membershipList{}
	err = g.conn.query(ctx, graphReadScopes, http.MethodGet, ps.Token, nil, resp)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			ctxzap.Extract(ctx).Warn(
				"group membership not found (underlying 404)",
				zap.String("group_id", resource.Id.GetResource()),
				zap.String("url", ps.Token),
				zap.Error(err),
			)
			return nil, "", nil, nil
		}

		return nil, "", nil, err
	}

	// dubious hack: if we get less than 50 members,
	// we suspect the NextLink will return an empty set.
	// this can save us ~50% of all requests when
	// looking at owners/members of small groups
	if len(resp.Members) <= 50 {
		resp.NextLink = ""
	}

	pageToken, err := b.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	grants, err := getGroupGrants(ctx, resp, resource, g, ps)
	if err != nil {
		return nil, "", nil, err
	}

	return grants, pageToken, nil, nil
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
		conn: c,
	}
}
