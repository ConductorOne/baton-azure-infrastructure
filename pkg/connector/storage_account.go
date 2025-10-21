package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/rolemapper"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

var typeEligible = "eligible"

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

	factory, err := armstorage.NewClientFactory(
		parentResourceID.Resource,
		usr.conn.token,
		usr.conn.client.ArmOptions(),
	)

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
			return storageAccountResource(ctx, account, parentResourceID, usr.conn.skipStorageContainerSync)
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

	options := []entitlement.EntitlementOption{
		entitlement.WithDisplayName(fmt.Sprintf("%s Eligible Member", resource.DisplayName)),
		entitlement.WithDescription(fmt.Sprintf("Eligible for %s group", resource.DisplayName)),
		entitlement.WithGrantableTo(userResourceType, groupResourceType),
		entitlement.WithAnnotation(&v2.EntitlementImmutable{}),
	}
	rv = append(rv, entitlement.NewAssignmentEntitlement(resource, typeEligible, options...))

	for _, value := range rolemapper.StorageAccountPermissions.Actions() {
		ent := entitlement.NewPermissionEntitlement(
			resource,
			value,
			entitlement.WithDisplayName(fmt.Sprintf("Can %s %s", value, resource.DisplayName)),
			entitlement.WithDescription(fmt.Sprintf("%s Storage account %s", value, resource.DisplayName)),
			entitlement.WithGrantableTo(roleResourceType),
			entitlement.WithAnnotation(&v2.EntitlementImmutable{}),
		)

		rv = append(rv, ent)
	}

	return rv, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (usr *storageAccountBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	type bagState struct {
		Value    string `json:"value"`
		State    string `json:"state"`
		NextLink string `json:"nextLink"`
	}

	l := ctxzap.Extract(ctx)

	// Stores RoleDefinitionIds
	bag := pagination.GenBag[bagState]{}

	err := bag.Unmarshal(pToken.Token)
	if err != nil {
		return nil, "", nil, err
	}

	storageResourceIDs, err := newStorageResourceSplitIdDataFromConnectorId(resource.Id.Resource)
	if err != nil {
		return nil, "", nil, err
	}

	if bag.Current() == nil {
		bag.Push(bagState{
			Value: "",
			State: "ELIGIBLE",
		})

		roleClient, err := armauthorization.NewRoleAssignmentsClient(
			storageResourceIDs.subscriptionID,
			usr.conn.token,
			usr.conn.client.ArmOptions(),
		)
		if err != nil {
			return nil, "", nil, err
		}

		var grants []*v2.Grant

		rolesAssignments := roleClient.NewListForScopePager(storageResourceIDs.AzureId(), nil)

		for rolesAssignments.More() {
			result, err := rolesAssignments.NextPage(ctx)
			if err != nil {
				return nil, "", nil, err
			}

			convertErr, err := ConvertErr(result.Value, func(in *armauthorization.RoleAssignment) (*v2.Grant, error) {
				bag.Push(bagState{
					Value: StringValue(in.Properties.RoleDefinitionID),
					State: "ASSIGNMENT",
				})

				return grantFromRoleAssigment(resource, "assignment", storageResourceIDs.subscriptionID, in)
			})

			if err != nil {
				return nil, "", nil, err
			}

			grants = append(grants, convertErr...)
		}

		nextToken, err := bag.Marshal()
		if err != nil {
			return nil, "", nil, err
		}

		return grants, nextToken, nil, nil
	}

	// Get the current state
	state := bag.Pop()

	var grants []*v2.Grant

	switch state.State {
	case "ASSIGNMENT":
		roleDefinitionId := state.Value
		roleDefinition, err := usr.conn.roleDefinitionsClient.GetByID(ctx, roleDefinitionId, nil)

		if err != nil {
			return nil, "", nil, err
		}

		actions, err := rolemapper.StorageAccountPermissions.MapRoleToAzureRoleAction(roleDefinition.Properties.Permissions)
		if err != nil {
			return nil, "", nil, err
		}

		for _, action := range actions {
			plainRoleId, err := roleIdFromRoleDefinitionId(roleDefinitionId)
			if err != nil {
				return nil, "", nil, err
			}

			roleResourceId, err := rs.NewResourceID(
				roleResourceType,
				fmt.Sprintf("%s:%s", plainRoleId, storageResourceIDs.subscriptionID),
			)

			if err != nil {
				return nil, "", nil, err
			}

			newGrant, err := grantFromRole(resource, action, roleResourceId)
			if err != nil {
				return nil, "", nil, err
			}

			grants = append(grants, newGrant)
		}

	case "ELIGIBLE":
		privilegedId := state.Value
		if privilegedId == "" {
			privilegedAccess, err := usr.conn.client.GetPrivilegedAccessFromAzure(ctx, storageResourceIDs.AzureId())
			if err != nil {
				if status.Code(err) == codes.NotFound {
					l.Warn("Privileged access not found", zap.String("scope", storageResourceIDs.AzureId()))
					return nil, "", nil, nil
				}

				if status.Code(err) == codes.PermissionDenied {
					l.Error("Permission denied for get privileged access", zap.String("scope", storageResourceIDs.AzureId()))
					return nil, "", nil, nil
				}

				// The tenant needs to have Microsoft Entra ID P2 or Microsoft Entra ID Governance license in order to request data to '/privilegedAccess/' API.
				if status.Code(err) == codes.Unknown && errorIsPremiumLicenseRequired(err) {
					errorMessage := getDetailedErrorMessage(err)
					l.Error("Permission denied for get privileged access. Premium License on Tenant is required",
						zap.String("scope", storageResourceIDs.AzureId()),
						zap.String("message", errorMessage),
					)
					return nil, "", nil, nil
				}

				return nil, "", nil, err
			}

			if privilegedAccess == nil {
				return nil, "", nil, fmt.Errorf("privileged access not found for scope %s", storageResourceIDs.AzureId())
			}

			privilegedId = privilegedAccess.Id
		}

		privilegedAssignments, nextLink, err := usr.conn.client.GetPrivilegedAccessRoleAssignments(ctx, privilegedId, state.NextLink)
		if err != nil {
			if status.Code(err) == codes.PermissionDenied {
				l.Error("Permission denied for get privileged access roles", zap.String("scope", privilegedId))
				return nil, "", nil, nil
			}

			// The tenant needs to have Microsoft Entra ID P2 or Microsoft Entra ID Governance license in order to request data to '/privilegedAccess/' API.
			if status.Code(err) == codes.Unknown && errorIsPremiumLicenseRequired(err) {
				errorMessage := getDetailedErrorMessage(err)
				l.Error("Permission denied for get privileged access. Premium License on Tenant is required", zap.String("scope", storageResourceIDs.AzureId()), zap.String("message", errorMessage))
				return nil, "", nil, nil
			}

			return nil, "", nil, err
		}

		grantsResponse, err := ConvertErr(privilegedAssignments, func(in client.PMIRoleAssigment) (*v2.Grant, error) {
			return grantFromEligibleAssignment(ctx, resource, in)
		})
		if err != nil {
			return nil, "", nil, err
		}

		if nextLink != "" {
			bag.Push(bagState{
				Value:    privilegedId,
				State:    "ELIGIBLE",
				NextLink: nextLink,
			})
		}

		grants = append(grants, grantsResponse...)

	default:
		return nil, "", nil, fmt.Errorf("unknown state: %s", state.State)
	}

	nextToken, err := bag.Marshal()
	if err != nil {
		return nil, "", nil, err
	}

	return grants, nextToken, nil, nil
}

func newStorageAccountBuilder(conn *Connector) *storageAccountBuilder {
	return &storageAccountBuilder{
		conn: conn,
	}
}
