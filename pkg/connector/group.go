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
	cn                         *Connector
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
		if g.cn.SkipAdGroups {
			v.Set("$filter", "(onPremisesSyncEnabled ne true)")
			v.Set("$count", "true") // Required to prevent MS Graph from returning a 400
		}

		reqURL = g.cn.buildURL("groups", v)
	}

	resp := &groupsList{}
	err = g.cn.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
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
	rv = append(rv, ent.NewPermissionEntitlement(resource, "owners", options...))

	options = []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Group Member", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Member of %s group", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType, groupResourceType),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, "members", options...))

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
		ownersURL := g.cn.buildBetaURL(path.Join("groups", resource.Id.Resource, "owners"), owenrsQuery)
		b.Push(pagination.PageState{
			ResourceTypeID: "owners",
			Token:          ownersURL,
		})

		memberQuery := setMemberQuery()
		if g.cn.SkipAdGroups {
			memberQuery.Set("$filter", "(onPremisesSyncEnabled ne true)")
			memberQuery.Set("$count", "true") // Required to prevent MS Graph from returning a 400
		}

		membersURL := g.cn.buildBetaURL(path.Join("groups", resource.Id.Resource, "members"), memberQuery)
		b.Push(pagination.PageState{
			ResourceTypeID: "members",
			Token:          membersURL,
		})
	}

	ps := b.Current()
	resp := &membershipList{}
	err = g.cn.query(ctx, graphReadScopes, http.MethodGet, ps.Token, nil, resp)
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

	grants, err := slices.ConvertErr(resp.Members, func(gm *membership) (*v2.Grant, error) {
		var annos annotations.Annotations
		objectID := resource.Id.GetResource()
		rid := &v2.ResourceId{Resource: gm.Id}
		switch gm.Type {
		case odataTypeGroup:
			rid.ResourceType = groupResourceType.Id
			annos.Update(&v2.GrantExpandable{
				EntitlementIds: []string{
					fmt.Sprintf("group:%s:members", rid.Resource),
				},
			})
		case odataTypeUser:
			rid.ResourceType = userResourceType.Id
		case odataTypeServicePrincipal:
			switch gm.ServicePrincipalType {
			case spTypeApplication:
				rid.ResourceType = enterpriseApplicationResourceType.Id
			case spTypeManagedIdentity:
				rid.ResourceType = managedIdentitylResourceType.Id
			case spTypeLegacy, spTypeSocialIdp, "":
				// https://learn.microsoft.com/en-us/graph/api/resources/serviceprincipal?view=graph-rest-1.0
				fallthrough
			default:
				if !g.knownServicePrincipalTypes[gm.ServicePrincipalType] {
					// Only log once per sync per type, to reduce log spam and datadog costs
					ctxzap.Extract(ctx).Warn(
						"Grants: unsupported ServicePrincipalType type on Group Membership",
						zap.String("type", gm.ServicePrincipalType),
						zap.String("objectID", objectID),
						zap.Any("membership", gm),
					)
					g.knownServicePrincipalTypes[gm.ServicePrincipalType] = true
				}

				return nil, nil
			}
		default:
			if !g.knownGroupMembershipTypes[gm.Type] {
				// Only log once per sync per type, to reduce log spam and datadog costs
				ctxzap.Extract(ctx).Warn(
					"Grants: unsupported resource type on Group Membership",
					zap.String("type", gm.Type),
					zap.String("objectID", objectID),
					zap.Any("membership", gm),
				)
				g.knownGroupMembershipTypes[gm.Type] = true
			}
			return nil, nil
		}
		ur := &v2.Resource{Id: rid}
		return &v2.Grant{
			Id: fmtResourceGrant(resource.Id, ur.Id, objectID+":"+ps.ResourceTypeID),
			Entitlement: &v2.Entitlement{
				Id:       fmt.Sprintf("group:%s:%s", resource.Id.Resource, ps.ResourceTypeID),
				Resource: resource,
			},
			Principal:   ur,
			Annotations: annos,
		}, nil
	})

	if err != nil {
		return nil, "", nil, err
	}

	return grants, pageToken, nil, nil
}

func newGroupBuilder(conn *Connector) *groupBuilder {
	return &groupBuilder{cn: conn}
}
