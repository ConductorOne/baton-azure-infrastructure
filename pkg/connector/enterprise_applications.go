package connector

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type enterpriseApplicationsBuilder struct {
	client *client.AzureClient
	cache  *GenericCache[*client.ServicePrincipal]

	// organizationIDs is a map of organization IDs that the user is a member of. needs to be set on the builder
	organizationIDs map[string]struct{}
	skipAdGroups    bool
}

func (e *enterpriseApplicationsBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return enterpriseApplicationResourceType
}

func (e *enterpriseApplicationsBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pt *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	bag, err := parsePageToken(pt.Token, &v2.ResourceId{ResourceType: userResourceType.Id})
	if err != nil {
		return nil, "", nil, err
	}

	reqURL := bag.PageToken()

	resp, err := e.client.ListServicePrincipals(ctx, reqURL)
	if err != nil {
		return nil, "", nil, err
	}

	var applicationsOwned []*client.ServicePrincipal

	for _, sp := range resp.Value {
		if _, ok := e.organizationIDs[sp.AppOwnerOrganizationId]; ok {
			e.cache.Set(sp.ID, sp)
			applicationsOwned = append(applicationsOwned, sp)
		}
	}

	resources := make([]*v2.Resource, 0, len(applicationsOwned))

	for i, app := range applicationsOwned {
		value, err := enterpriseApplicationResource(ctx, app, parentResourceID)
		if err != nil {
			return nil, "", nil, err
		}

		resources[i] = value
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	return resources, pageToken, nil, nil
}

func (e *enterpriseApplicationsBuilder) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	ownersEntId := enterpriseApplicationsEntitlementId{
		Type:     ownersStr,
		Resource: resource.Id.Resource,
	}

	// Most people are assigned directly to app roles but some people could be assigned
	// to the app directly
	// normally this happens by assigning someone access to an app while the app has roles
	// but it's possible the app then gets roles, meaning we have someone with the default assignment
	// and then someone with a specific role assignment
	defaultAppRoleAssignmentStringer := enterpriseApplicationsEntitlementId{
		Type:      "appRole",
		Resource:  resource.Id.Resource,
		AppRoleId: defaultAppRoleAssignmentID,
	}

	var err error
	ownersEntIdString, err := ownersEntId.MarshalString()
	if err != nil {
		return nil, "", nil, err
	}

	defaultAppRoleAssignmentStringerString, err := defaultAppRoleAssignmentStringer.MarshalString()
	if err != nil {
		return nil, "", nil, err
	}

	// https://learn.microsoft.com/en-us/graph/api/resources/approleassignment?view=graph-rest-1.0
	rv := []*v2.Entitlement{
		{
			Id:          ownersEntIdString,
			Resource:    resource,
			DisplayName: fmt.Sprintf("%s Application Owner", resource.DisplayName),
			Description: fmt.Sprintf("Owner of %s Application", resource.DisplayName),
			GrantableTo: []*v2.ResourceType{userResourceType},
			Purpose:     v2.Entitlement_PURPOSE_VALUE_PERMISSION,
			Slug:        "owner",
		},
		// NOTE:
		// "00000000-0000-0000-0000-000000000000" is the principal ID for the default app role.
		{
			Id:          defaultAppRoleAssignmentStringerString,
			Resource:    resource,
			DisplayName: fmt.Sprintf("%s Application Assignment", resource.DisplayName),
			Description: fmt.Sprintf("Assigned to %s Application", resource.DisplayName),
			GrantableTo: []*v2.ResourceType{userResourceType, groupResourceType},
			Purpose:     v2.Entitlement_PURPOSE_VALUE_ASSIGNMENT,
			Slug:        "assigned",
		},
	}

	principalId := resource.Id.Resource
	servicePrincipal, err := e.cache.GetOrSet(principalId, func() (*client.ServicePrincipal, error) {
		return e.client.ServicePrincipal(ctx, principalId)
	})

	if err != nil {
		return nil, "", nil, fmt.Errorf("baton-azure-infrastructure: failed to get service principal on cache: %w", err)
	}

	for _, appRole := range servicePrincipal.AppRoles {
		// TODO: Needs to validate this rule
		if !slices.Contains(appRole.AllowedMemberTypes, "User") {
			continue
		}

		appRoleAssignmentId := enterpriseApplicationsEntitlementId{
			Type:      "appRole",
			Resource:  resource.Id.Resource,
			AppRoleId: appRole.Id,
		}

		slug := appRole.Value
		if slug == "" {
			slug = appRole.DisplayName
		}

		appRoleAssignmentIdString, err := appRoleAssignmentId.MarshalString()
		if err != nil {
			return nil, "", nil, err
		}

		ent := entitlement.NewAssignmentEntitlement(
			resource,
			appRoleAssignmentIdString,
			entitlement.WithGrantableTo(userResourceType, groupResourceType),
			entitlement.WithDisplayName(fmt.Sprintf("%s Role Assignment", appRole.DisplayName)),
			entitlement.WithDescription(fmt.Sprintf("Assigned to %s Application with %s Role", resource.DisplayName, appRole.Description)),
		)
		ent.Slug = slug

		rv = append(rv, ent)
	}

	return rv, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (e *enterpriseApplicationsBuilder) Grants(ctx context.Context, resource *v2.Resource, pt *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	b := &pagination.Bag{}
	err := b.Unmarshal(pt.Token)
	if err != nil {
		return nil, "", nil, err
	}

	// Id relarted to Azure resource
	principalId := strings.TrimPrefix(resource.Id.Resource, "applications/")

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
		b.Push(pagination.PageState{
			ResourceTypeID: ownersStr,
		})
		b.Push(pagination.PageState{
			ResourceTypeID: assignmentStr,
		})
	}

	ps := b.Current()
	switch ps.ResourceTypeID {
	case assignmentStr:
		principalResp, err := e.cache.GetOrSet(principalId, func() (*client.ServicePrincipal, error) {
			return e.client.ServicePrincipal(ctx, principalId)
		})
		if err != nil {
			return nil, "", nil, err
		}

		resp := principalResp.AppRolesAssignedTo
		grants, err := ConvertErr(resp, func(ara *client.AppRoleAssignment) (*v2.Grant, error) {
			var annos annotations.Annotations
			rid := &v2.ResourceId{Resource: ara.PrincipalId}
			switch ara.PrincipalType {
			case "User":
				rid.ResourceType = userResourceType.Id
			case "Group":
				rid.ResourceType = groupResourceType.Id
				annos.Update(&v2.GrantExpandable{
					EntitlementIds: []string{
						fmt.Sprintf("group:%s:members", ara.PrincipalId),
					},
					Shallow:         true,
					ResourceTypeIds: []string{userResourceType.Id},
				})
			case "ServicePrincipal":
				// TODO: service principals can be managed identities, enterprise applications, or maybe something else entirely.
				// We need to figure out the resource type instead of hard coding it to be a managed identity.
				rid.ResourceType = managedIdentitylResourceType.Id
				// rid.ResourceType = enterpriseApplicationResourceType.Id
			}
			ur := &v2.Resource{Id: rid}
			return &v2.Grant{
				Id: ara.Id,
				Entitlement: &v2.Entitlement{
					Id: fmt.Sprintf("enterprise_application:%s:assignment:%s",
						resource.Id.Resource,
						ara.AppRoleId,
					),
					Resource: resource,
				},
				Principal:   ur,
				Annotations: annos,
			}, nil
		})
		if err != nil {
			return nil, "", nil, err
		}
		return grants, "", nil, err
	case ownersStr:

		resp, err := e.client.ServicePrincipalOwners(ctx, principalId)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				ctxzap.Extract(ctx).Warn(
					"app role owner membership not found (underlying 404)",
					zap.String("app_role_assignment_id", resource.Id.GetResource()),
					zap.String("url", ps.Token),
					zap.Error(err),
				)
				return nil, "", nil, nil
			}

			return nil, "", nil, err
		}

		pageToken, err := b.NextToken(resp.NextLink)
		if err != nil {
			return nil, "", nil, err
		}

		grants, err := ConvertErr(resp.Members, func(gm *client.Membership) (*v2.Grant, error) {
			objectID := resource.Id.GetResource()
			rid := &v2.ResourceId{Resource: gm.Id}
			switch gm.Type {
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
					ctxzap.Extract(ctx).Warn(
						"Grants: unsupported ServicePrincipalType type on app owner Membership",
						zap.String("type", gm.ServicePrincipalType),
						zap.String("objectID", objectID),
						zap.Any("membership", gm),
					)
					return nil, nil
				}
			default:
				return nil, fmt.Errorf("unknown membership type %+v for application owner (id=%s)", gm, objectID)
			}

			ur := &v2.Resource{Id: rid}
			return &v2.Grant{
				Id: gm.Id,
				Entitlement: &v2.Entitlement{
					Id:       fmt.Sprintf("enterprise_application:%s:owners", resource.Id.Resource),
					Resource: resource,
				},
				Principal: ur,
			}, nil
		})

		if err != nil {
			return nil, "", nil, err
		}

		return grants, pageToken, nil, nil
	default:
		return nil, "", nil, fmt.Errorf("unknown resource type: %s", ps.ResourceTypeID)
	}
}

func newEnterpriseApplicationsBuilder(c *Connector) *enterpriseApplicationsBuilder {
	organizationIDs := map[string]struct{}{}

	for _, d := range c.organizationIDs {
		organizationIDs[d] = struct{}{}
	}

	return &enterpriseApplicationsBuilder{
		client:          c.client,
		cache:           NewGenericCache[*client.ServicePrincipal](),
		organizationIDs: organizationIDs,
		skipAdGroups:    c.SkipAdGroups,
	}
}

type enterpriseApplicationsEntitlementId struct {
	Type      string
	Resource  string
	AppRoleId string
}

func (id *enterpriseApplicationsEntitlementId) MarshalString() (string, error) {
	switch id.Type {
	case appRoleStr:
		return strings.Join(
			[]string{
				"enterprise_application",
				id.Resource,
				assignmentStr,
				id.AppRoleId,
			},
			":"), nil
	case ownersStr:
		return strings.Join(
			[]string{
				"enterprise_application",
				id.Resource,
				ownersStr,
			},
			":"), nil
	default:
		return "", fmt.Errorf("unknown entitlement type: %s", id.Type)
	}
}

func (id *enterpriseApplicationsEntitlementId) UnmarshalString(input string) error {
	parts := strings.Split(input, ":")
	if len(parts) < 3 {
		return errors.New("baton-microsoft-entra: invalid entitlement id")
	}
	id.Type = parts[2]
	id.Resource = parts[1]
	if id.Type == assignmentStr {
		if len(parts) < 4 {
			return errors.New("baton-microsoft-entra: invalid entitlement id: missing approle id")
		}
		id.AppRoleId = parts[3]
	}
	return nil
}

func (o *enterpriseApplicationsBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	eaEntId := &enterpriseApplicationsEntitlementId{}
	err := eaEntId.UnmarshalString(entitlement.Id)
	if err != nil {
		return nil, err
	}

	l := ctxzap.Extract(ctx)
	if principal.Id.ResourceType != userResourceType.Id {
		l.Warn(
			"baton-microsoft-entra: only users can be granted enterprise app entitlements",
			zap.String("principal_type", principal.Id.ResourceType),
			zap.String("principal_id", principal.Id.Resource),
		)
		return nil, errors.New("baton-microsoft-entra: only users can be granted enterprise app entitlements")
	}

	resourceID := entitlement.Resource.Id.Resource
	switch eaEntId.Type {
	case "owners":
		err := o.client.ServicePrincipalAddOwner(ctx, resourceID, principal.Id.Resource)
		if err != nil {
			return nil, err
		}

	case assignmentStr:
		err := o.client.ServicePrincipalGrantAppRoleAssignment(
			ctx,
			resourceID,
			eaEntId.AppRoleId,
			principal.Id.Resource,
		)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("baton-microsoft-entra: only can provision app roles or owners entitlements to an enterprise application")
	}

	return nil, nil
}

func (o *enterpriseApplicationsBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	eaEntId := &enterpriseApplicationsEntitlementId{}
	err := eaEntId.UnmarshalString(grant.Entitlement.Id)
	if err != nil {
		return nil, err
	}
	l := ctxzap.Extract(ctx)
	resourceID := grant.Entitlement.Resource.Id.Resource
	switch eaEntId.Type {
	case ownersStr:

		err := o.client.ServicePrincipalDeleteOwner(ctx, resourceID, grant.Principal.Id.Resource)
		if err != nil {
			return nil, err
		}
	case assignmentStr:
		err := o.client.ServicePrincipalDeleteAppRoleAssignedTo(ctx, resourceID, grant.Id)
		if err != nil {
			return nil, err
		}
	default:
		l.Warn(
			"baton-microsoft-entra: only can revoke app roles or owners entitlements to an enterprise application",
			zap.String("entitlement_id", grant.Entitlement.Id),
		)
		return nil, errors.New("baton-microsoft-entra: only can revoke app roles or owners entitlements to an enterprise application")
	}

	return nil, nil
}
