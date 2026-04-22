package connector

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/rolemapper"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
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

func (usr *containerBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	if parentResourceID == nil {
		return nil, nil, nil
	}

	if parentResourceID.ResourceType != storageAccountResourceType.Id {
		return nil, nil, fmt.Errorf("invalid resource type: %s", parentResourceID.ResourceType)
	}

	storageId, err := newStorageResourceSplitIdDataFromConnectorId(parentResourceID.Resource)
	if err != nil {
		return nil, nil, err
	}

	factory, err := armstorage.NewClientFactory(
		storageId.subscriptionID,
		usr.conn.token,
		usr.conn.client.ArmOptions(),
	)

	if err != nil {
		return nil, nil, err
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
			return nil, nil, err
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
			return nil, nil, err
		}

		resources = append(resources, temp...)
	}

	return resources, nil, nil
}

// Entitlements always returns an empty slice for users.
func (usr *containerBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
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

	for _, action := range rolemapper.ContainerPermissions.Actions() {
		ent := entitlement.NewPermissionEntitlement(
			resource,
			action,
			entitlement.WithDisplayName(fmt.Sprintf("Can %s %s", action, resource.DisplayName)),
			entitlement.WithDescription(fmt.Sprintf("Can %s %s", action, resource.DisplayName)),
			entitlement.WithGrantableTo(roleResourceType),
			entitlement.WithAnnotation(&v2.EntitlementImmutable{}),
		)

		rv = append(rv, ent)
	}

	return rv, nil, nil
}

// getRoleDefinition resolves a role definition by ID, consulting the
// session-store cache first and falling through to a live Graph lookup on
// miss. opts.Session may be nil in test harnesses; in that case we always
// hit Graph.
// Grants returns no grants. Access to containers is authoritatively
// expressed by role_assignment resources with ScopeBindingTrait whose
// scope_resource_id references either the container itself or an ancestor
// scope (storage account / resource group / subscription / management
// group). The pre-sparse-ACL implementation emitted grants on container
// action entitlements with role resources as principal and GrantExpandable
// annotations — dead projections now that per-role entitlements are gone.
// See role_assignment.go for the authoritative path.
func (usr *containerBuilder) Grants(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func newContainerBuilder(conn *Connector) *containerBuilder {
	return &containerBuilder{
		conn:   conn,
		client: conn.client,
	}
}
