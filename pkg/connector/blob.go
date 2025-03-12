package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	azContainer "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type blobBuilder struct {
	conn *Connector
}

func (usr *blobBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return blobResourceType
}

func (usr *blobBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	if parentResourceID == nil {
		return nil, "", nil, nil
	}

	// TODO: disable until I get access to blobs
	return nil, "", nil, nil

	storageAccountIdSplit := strings.Split(parentResourceID.Resource, ":")

	if len(storageAccountIdSplit) != 2 {
		return nil, "", nil, fmt.Errorf("invalid resource id: %s", parentResourceID.Resource)
	}

	storageAccountName := storageAccountIdSplit[0]
	containerName := storageAccountIdSplit[1]

	serviceUrl := fmt.Sprintf(serviceUrlTemplate, storageAccountName)

	azBlobClient, err := azblob.NewClient(serviceUrl, usr.conn.token, nil)
	if err != nil {
		return nil, "", nil, err
	}

	resources := make([]*v2.Resource, 0)

	var options *azblob.ListBlobsFlatOptions
	if pToken.Token != "" {
		options = &azblob.ListBlobsFlatOptions{
			Marker: &pToken.Token,
		}
	}

	pager := azBlobClient.NewListBlobsFlatPager(containerName, options)

	result, err := pager.NextPage(ctx)
	if err != nil {
		return nil, "", nil, err
	}

	convertErr, err := ConvertErr(result.Segment.BlobItems, func(in *azContainer.BlobItem) (*v2.Resource, error) {
		return blobResource(in, parentResourceID)
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
func (usr *blobBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (usr *blobBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func blobResource(blob *azContainer.BlobItem, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"version_id": StringValue(blob.VersionID),
		"snapshot":   StringValue(blob.Snapshot),
		"deleted":    BoolValue(blob.Deleted),
	}

	if blob.Properties != nil {
		profile["properties_owner"] = StringValue(blob.Properties.Owner)
		profile["properties_group"] = StringValue(blob.Properties.Group)
		profile["properties_permissions"] = StringValue(blob.Properties.Permissions)
	}

	appTraits := []rs.AppTraitOption{
		rs.WithAppProfile(profile),
	}

	return rs.NewResource(
		StringValue(blob.Name),
		blobResourceType,
		StringValue(blob.Name),
		rs.WithAppTrait(appTraits...),
		rs.WithParentResourceID(parentResourceID),
	)
}

func newBlobBuilder(conn *Connector) *blobBuilder {
	return &blobBuilder{
		conn: conn,
	}
}
