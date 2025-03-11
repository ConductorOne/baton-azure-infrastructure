package connector

import (
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	azService "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"strings"
)

var serviceUrlTemplate = "https://%s.blob.core.windows.net/"

// containerBuilder syncs Container given an StorageAccount
type containerBuilder struct {
	conn *Connector
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

	storageAccountIdSplit := strings.Split(parentResourceID.Resource, ":")

	if len(storageAccountIdSplit) != 5 {
		return nil, "", nil, fmt.Errorf("invalid resource id: %s", parentResourceID.Resource)
	}

	storageAccountName := storageAccountIdSplit[len(storageAccountIdSplit)-1]

	serviceUrl := fmt.Sprintf(serviceUrlTemplate, storageAccountName)

	azBlobClient, err := azblob.NewClient(serviceUrl, usr.conn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	resources := make([]*v2.Resource, 0)

	var options *azblob.ListContainersOptions
	if pToken.Token != "" {
		options = &azblob.ListContainersOptions{
			Marker: &pToken.Token,
		}
	}

	pager := azBlobClient.NewListContainersPager(options)

	result, err := pager.NextPage(ctx)
	if err != nil {
		return nil, "", nil, err
	}

	convertErr, err := ConvertErr(result.ContainerItems, func(in *azService.ContainerItem) (*v2.Resource, error) {
		return containerResource(storageAccountName, in, parentResourceID)
	})

	if err != nil {
		return nil, "", nil, err
	}

	resources = append(resources, convertErr...)

	var nextToken string
	if result.NextMarker != nil && len(*result.NextMarker) > 0 {
		nextToken = *result.NextMarker
	} else {
		nextToken = ""
	}

	return resources, nextToken, nil, nil
}

// Entitlements always returns an empty slice for users.
func (usr *containerBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (usr *containerBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func containerResource(storageAccountName string, container *azService.ContainerItem, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"version":                 StringValue(container.Version),
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
		fmt.Sprintf("%s:%s", storageAccountName, StringValue(container.Name)),
		rs.WithAppTrait(appTraits...),
		rs.WithParentResourceID(parentResourceID),
		rs.WithAnnotation(
			&v2.ChildResourceType{ResourceTypeId: blobResourceType.Id},
		),
	)
}

func newContainerBuilder(conn *Connector) *containerBuilder {
	return &containerBuilder{
		conn: conn,
	}
}
