package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	zap "go.uber.org/zap"
)

type groupBuilder struct {
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

func (g *groupBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, attrs resource.SyncOpAttrs) ([]*v2.Resource, *resource.SyncOpResults, error) {
	resp, err := g.client.Groups(ctx, attrs.PageToken.Token)
	if err != nil {
		return nil, nil, err
	}

	groups, err := ConvertErr(resp.Groups, func(g *client.Group) (*v2.Resource, error) {
		return groupResource(ctx, g, parentResourceID)
	})
	if err != nil {
		return nil, nil, err
	}

	return groups, &resource.SyncOpResults{NextPageToken: resp.NextLink}, nil
}

// Entitlements always returns an empty slice for users.
func (g *groupBuilder) Entitlements(_ context.Context, res *v2.Resource, _ resource.SyncOpAttrs) ([]*v2.Entitlement, *resource.SyncOpResults, error) {
	var rv []*v2.Entitlement
	options := []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Group Owner", res.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Owner of %s group", res.DisplayName)),
		ent.WithGrantableTo(userResourceType),
	}
	rv = append(rv, ent.NewPermissionEntitlement(res, typeOwners, options...))

	options = []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Group Member", res.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Member of %s group", res.DisplayName)),
		ent.WithGrantableTo(userResourceType, groupResourceType),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(res, typeMembers, options...))

	return rv, nil, nil
}

func (g *groupBuilder) Grants(ctx context.Context, res *v2.Resource, attrs resource.SyncOpAttrs) ([]*v2.Grant, *resource.SyncOpResults, error) {
	l := ctxzap.Extract(ctx)
	b := &resource.PaginationBag{}
	err := b.Unmarshal(attrs.PageToken.Token)
	if err != nil {
		return nil, nil, err
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
		b.Push(resource.PageState{
			ResourceTypeID: typeOwners,
		})

		b.Push(resource.PageState{
			ResourceTypeID: typeMembers,
		})
	}

	ps := b.Pop()
	if ps == nil {
		return nil, nil, nil
	}

	groupId := res.Id.Resource
	var memberShip *client.MembershipList

	switch ps.ResourceTypeID {
	case typeOwners:
		memberShip, err = g.client.GroupOwners(ctx, groupId)
	case typeMembers:
		memberShip, err = g.client.GroupMembers(ctx, groupId, ps.Token)
	default:
		return nil, nil, fmt.Errorf("baton-azure-infrastructure: unknown resource type ID %s", ps.ResourceTypeID)
	}

	if err != nil {
		if status.Code(err) == codes.NotFound {
			l.Warn(
				"group membership not found",
				zap.String("type", ps.ResourceTypeID),
				zap.String("group_id", groupId),
				zap.Error(err),
			)
			return nil, nil, nil
		}

		return nil, nil, err
	}

	// dubious hack: if we get less than 50 members,
	// we suspect the NextLink will return an empty set.
	// this can save us ~50% of all requests when
	// looking at owners/members of small groups
	if len(memberShip.Members) <= 50 {
		memberShip.NextLink = ""
	}

	if memberShip.NextLink != "" {
		b.Push(resource.PageState{
			ResourceTypeID: ps.ResourceTypeID,
			ResourceID:     ps.ResourceID,
			Token:          memberShip.NextLink,
		})
	}

	grants, err := getGroupGrants(ctx, memberShip, res, ps)
	if err != nil {
		return nil, nil, err
	}

	nextToken, err := b.Marshal()
	if err != nil {
		return nil, nil, err
	}

	return grants, &resource.SyncOpResults{NextPageToken: nextToken}, nil
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

	var err error
	groupID := entitlement.Resource.Id.Resource
	objRef := getGroupGrantURL(principal)

	switch {
	case strings.HasSuffix(entitlement.Id, ":owners"):
		err = g.client.GroupAddOwner(ctx, groupID, objRef)
	case strings.HasSuffix(entitlement.Id, ":members"):
		err = g.client.GroupAddMember(ctx, groupID, objRef)
	default:
		return nil, errors.New("baton-azure-infrastructure: only members can provision membership or owners entitlements to a group")
	}

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

	var err error
	groupID := entitlement.Resource.Id.Resource
	userID := principal.Id.Resource
	switch {
	case strings.HasSuffix(entitlement.Id, ":owners"):
		err = g.client.GroupRemoveOwner(ctx, groupID, userID)
	case strings.HasSuffix(entitlement.Id, ":members"):
		err = g.client.GroupRemoveMember(ctx, groupID, userID)
	default:
		return nil, errors.New("baton-azure-infrastructure: only can revoke membership or owners entitlements to a group")
	}
	if err != nil {
		if status.Code(err) == codes.NotFound {
			l.Info("Group membership to revoke not found; treating as successful because the end state is achieved")
			return nil, nil
		}

		return nil, err
	}

	return nil, nil
}

func newGroupBuilder(c *Connector) *groupBuilder {
	return &groupBuilder{
		client: c.client,
	}
}
