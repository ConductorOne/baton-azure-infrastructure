package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

// containerBuilder syncs Container given an StorageAccount.
type containerBuilder struct {
	client *client.AzureClient
	conn   *Connector
}

func (usr *containerBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return containerResourceType
}

func (usr *containerBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	if parentResourceID == nil {
		return nil, "", nil, nil
	}

	if parentResourceID.ResourceType != storageAccountResourceType.Id {
		return nil, "", nil, fmt.Errorf("invalid resource type: %s", parentResourceID.ResourceType)
	}

	storageId, err := newStorageResourceSplitIdDataFromConnectorId(parentResourceID.Resource)
	if err != nil {
		return nil, "", nil, err
	}

	factory, err := armstorage.NewClientFactory(
		storageId.subscriptionID,
		usr.conn.token,
		usr.conn.client.ArmOptions(),
	)

	if err != nil {
		return nil, "", nil, err
	}

	pager := factory.NewBlobContainersClient().
		NewListPager(
			storageId.resourceGroupName,
			storageId.resourceName,
			nil,
		)

	resources := make([]*v2.Resource, 0)

	for pager.More() {
		result, err := pager.NextPage(ctx)
		if err != nil {
			return nil, "", nil, err
		}

		temp, err := ConvertErr(result.Value, func(container *armstorage.ListContainerItem) (*v2.Resource, error) {
			profile := map[string]interface{}{
				"type":                    StringValue(container.Type),
				"has_immutability_policy": BoolValue(container.Properties.HasImmutabilityPolicy),
				"has_legal_hold":          BoolValue(container.Properties.HasLegalHold),
			}

			if container.Properties.PublicAccess != nil {
				profile["properties_public_access"] = string(*container.Properties.PublicAccess)
			}

			appTraits := []rs.AppTraitOption{
				rs.WithAppProfile(profile),
			}

			return rs.NewResource(
				StringValue(container.Name),
				containerResourceType,
				fmt.Sprintf("%s:%s", storageId.resourceName, StringValue(container.Name)),
				rs.WithAppTrait(appTraits...),
				rs.WithParentResourceID(parentResourceID),
			)
		})

		if err != nil {
			return nil, "", nil, err
		}

		resources = append(resources, temp...)
	}

	return resources, "", nil, nil
}

// Entitlements always returns an empty slice for users.
func (usr *containerBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
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
func (usr *containerBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	if resource.ParentResourceId == nil || resource.ParentResourceId.ResourceType != storageAccountResourceType.Id {
		return nil, "", nil, fmt.Errorf("container resource must have a parent resource from type %s", storageAccountResourceType.Id)
	}

	parsedParentId, err := newStorageResourceSplitIdDataFromConnectorId(resource.ParentResourceId.Resource)
	if err != nil {
		return nil, "", nil, err
	}

	idSplit := strings.Split(resource.Id.Resource, ":")
	if len(idSplit) != 2 {
		return nil, "", nil, fmt.Errorf("invalid resource id: %s", resource.Id.Resource)
	}

	containerName := idSplit[1]

	scope := fmt.Sprintf("%s/blobServices/default/containers/%s", parsedParentId.AzureId(), containerName)
	assignments, err := usr.client.GetRoleAssignments(ctx, parsedParentId.subscriptionID, scope)
	if err != nil {
		return nil, "", nil, err
	}

	grants, err := ConvertErr(assignments, func(in *armauthorization.RoleAssignment) (*v2.Grant, error) {
		return grantFromRoleAssigment(
			resource,
			"assignment",
			parsedParentId.subscriptionID,
			in,
		)
	})
	if err != nil {
		return nil, "", nil, err
	}

	return grants, "", nil, nil
}

func newContainerBuilder(conn *Connector) *containerBuilder {
	return &containerBuilder{
		conn:   conn,
		client: conn.client,
	}
}
