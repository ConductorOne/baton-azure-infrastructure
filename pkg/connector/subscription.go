package connector

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	grant "github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type subscriptionBuilder struct {
	cn *Connector
}

func (s *subscriptionBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return subscriptionsResourceType
}

func (s *subscriptionBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource
	pager := s.cn.clientFactory.NewSubscriptionsClient().NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, subscription := range page.Value {
			sr, err := subscriptionResource(ctx, subscription)
			if err != nil {
				return nil, "", nil, err
			}

			rv = append(rv, sr)
		}
	}

	return rv, "", nil, nil
}

func (s *subscriptionBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	options := []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Subscription Owner", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Owner of %s subscription", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType),
	}
	rv = append(rv, ent.NewPermissionEntitlement(resource, typeOwners, options...))

	options = []ent.EntitlementOption{
		ent.WithDisplayName(fmt.Sprintf("%s Subscription Member", resource.DisplayName)),
		ent.WithDescription(fmt.Sprintf("Member of %s subscription", resource.DisplayName)),
		ent.WithGrantableTo(userResourceType, groupResourceType),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, typeMembers, options...))

	return rv, "", nil, nil
}

func (s *subscriptionBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var (
		rv          []*v2.Grant
		gr          *v2.Grant
		roleID      string
		principalId *v2.ResourceId
		isUserType  = true
	)
	subscriptionID := resource.Id.Resource
	// Create a new RoleAssignmentsClient
	client, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, s.cn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	// Iterate over all role assignments
	pager := client.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		for _, assignment := range page.Value {
			isResourceGroupType, err := isResourceGroup(s.cn.token, subscriptionID, *assignment.Properties.PrincipalID)
			if err != nil {
				return nil, "", nil, err
			}

			if isResourceGroupType {
				principalId = &v2.ResourceId{
					ResourceType: resourceGroupResourceType.Id,
					Resource:     *assignment.Properties.PrincipalID,
				}
				isUserType = false
			}

			if isUserType {
				principalId = &v2.ResourceId{
					ResourceType: userResourceType.Id,
					Resource:     *assignment.Properties.PrincipalID,
				}
			}

			roleDefinitionID := assignment.Properties.RoleDefinitionID
			roleID = getRoleId(roleDefinitionID)
			roleRes, err := rs.NewResource(
				*assignment.Name,
				roleResourceType,
				roleID,
			)
			if err != nil {
				return nil, "", nil, err
			}

			gr = grant.NewGrant(roleRes, typeMembers, principalId)
			rv = append(rv, gr)
		}
	}

	return rv, "", nil, nil
}

func newSubscriptionBuilder(conn *Connector) *subscriptionBuilder {
	return &subscriptionBuilder{
		cn: conn,
	}
}
