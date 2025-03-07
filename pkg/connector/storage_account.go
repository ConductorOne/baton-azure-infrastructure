package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type storageAccountBuilder struct {
	conn *Connector
}

func (usr *storageAccountBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return storageAccountResourceType
}

// List returns all the users from the database as resource objects.
// Users include a UserTrait because they are the 'shape' of a standard user.
func (usr *storageAccountBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	if parentResourceID == nil {
		return nil, "", nil, nil
	}

	if parentResourceID.ResourceType != subscriptionsResourceType.Id {
		return nil, "", nil, fmt.Errorf("parentResourceID.ResourceType is not supported: %s", parentResourceID.ResourceType)
	}

	factory, err := armstorage.NewClientFactory(parentResourceID.Resource, usr.conn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	storageClient := factory.NewAccountsClient()

	storageAccounts := storageClient.NewListPager(nil)

	var resources []*v2.Resource

	for storageAccounts.More() {
		response, err := storageAccounts.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		convertErr, err := ConvertErr(response.Value, func(account *armstorage.Account) (*v2.Resource, error) {
			return storageAccountResource(ctx, account, parentResourceID)
		})

		if err != nil {
			return nil, "", nil, err
		}

		resources = append(resources, convertErr...)
	}

	return resources, "", nil, nil
}

// Entitlements always returns an empty slice for users.
func (usr *storageAccountBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	rv := []*v2.Entitlement{
		entitlement.NewPermissionEntitlement(
			resource,
			"assignment",
			entitlement.WithDisplayName(fmt.Sprintf("Access to %s", resource.DisplayName)),
			entitlement.WithDescription(fmt.Sprintf("Access to %s", resource.DisplayName)),
			entitlement.WithGrantableTo(roleResourceType),
			entitlement.WithAnnotation(&v2.EntitlementImmutable{}),
		),
	}

	return rv, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (usr *storageAccountBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	storageResourceIDs, err := newStorageResourceSplitIdDataFromConnectorId(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, err
	}

	client, err := armauthorization.NewRoleAssignmentsClient(
		storageResourceIDs.subscriptionID,
		usr.conn.token,
		nil,
	)
	if err != nil {
		return nil, "", nil, err
	}

	var grants []*v2.Grant

	rolesAssignments := client.NewListForScopePager(storageResourceIDs.AzureId(), nil)

	for rolesAssignments.More() {
		result, err := rolesAssignments.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		convertErr, err := ConvertErr(result.Value, func(in *armauthorization.RoleAssignment) (*v2.Grant, error) {
			if in.Properties.RoleDefinitionID == nil {
				return nil, fmt.Errorf("role definition id is nil")
			}

			// RoleDefinitionID example value
			// /subscriptions/{subscriptionId}/providers/Microsoft.Authorization/roleDefinitions/{roleId}
			splitValues := strings.Split(StringValue(in.Properties.RoleDefinitionID), "/")
			if len(splitValues) != 7 {
				return nil, fmt.Errorf("invalid role definition id %s", StringValue(in.Properties.RoleDefinitionID))
			}
			roleIdFromSplit := splitValues[len(splitValues)-1]

			// roleID : subscriptionID
			roleId, err := rs.NewResourceID(
				roleResourceType,
				fmt.Sprintf("%s:%s", roleIdFromSplit, storageResourceIDs.subscriptionID),
			)
			if err != nil {
				return nil, err
			}

			grantOpts := []grant.GrantOption{}
			// TODO: review this grant Expandable operation
			grantOpts = append(grantOpts, grant.WithAnnotation(&v2.GrantExpandable{
				EntitlementIds: []string{
					fmt.Sprintf("role:%s:owners", roleId.Resource),
					fmt.Sprintf("role:%s:assigned", roleId.Resource),
				},
				Shallow: true,
			}))

			return grant.NewGrant(
				resource,
				"assignment",
				roleId,
				grantOpts...,
			), nil
		})

		if err != nil {
			return nil, "", nil, err
		}

		grants = append(grants, convertErr...)
	}

	return grants, "", nil, nil
}

func newStorageAccountBuilder(conn *Connector) *storageAccountBuilder {
	return &storageAccountBuilder{
		conn: conn,
	}
}
