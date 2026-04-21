package connector

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/conductorone/baton-sdk/pkg/types/grant"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/session"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	ownersStr     = "owners"
	appRoleStr    = "appRole"
	assignmentStr = "assignment"

	// servicePrincipalPrefix is the session-store key prefix for the
	// service-principal metadata cache. Service principal documents are
	// fetched once and reused across List / Entitlements / Grants to avoid
	// N re-lookups against Graph for the same application.
	servicePrincipalPrefix = "azinfra-ent-app-sp:"
)

type enterpriseApplicationsBuilder struct {
	client       *client.AzureClient
	conn         *Connector
	skipAdGroups bool
}

// spFromSession fetches the cached ServicePrincipal via opts.Session; falls
// back to a live Graph lookup on miss and populates the cache. Returns
// (nil, nil) only if Graph itself returns empty (shouldn't happen — Graph
// errors propagate instead).
func (e *enterpriseApplicationsBuilder) spFromSession(ctx context.Context, opts rs.SyncOpAttrs, principalID string) (*client.ServicePrincipal, error) {
	if opts.Session != nil {
		if sp, found, err := session.GetJSON[*client.ServicePrincipal](ctx, opts.Session, principalID, sessions.WithPrefix(servicePrincipalPrefix)); err == nil && found {
			return sp, nil
		}
	}
	sp, err := e.client.ServicePrincipal(ctx, principalID)
	if err != nil {
		return nil, err
	}
	if opts.Session != nil {
		_ = session.SetJSON(ctx, opts.Session, principalID, sp, sessions.WithPrefix(servicePrincipalPrefix))
	}
	return sp, nil
}

func (e *enterpriseApplicationsBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return enterpriseApplicationResourceType
}

func (e *enterpriseApplicationsBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	bag, err := parsePageToken(opts.PageToken.Token, &v2.ResourceId{ResourceType: userResourceType.Id})
	if err != nil {
		return nil, nil, err
	}

	reqURL := bag.PageToken()

	resp, err := e.client.ListServicePrincipals(ctx, reqURL)
	if err != nil {
		return nil, nil, err
	}

	// Lazily fetch the set of organization IDs the SP can see. Memoized via
	// sync.Once on the Connector — deferred out of New() so `capabilities`
	// generation works without Azure credentials.
	orgIDs, err := e.conn.organizationIDs(ctx)
	if err != nil {
		return nil, nil, err
	}

	var applicationsOwned []*client.ServicePrincipal

	// Bulk-populate the session-store cache with this page's service
	// principals so subsequent Entitlements / Grants calls skip the
	// per-principal Graph roundtrip. When opts.Session is nil (test
	// harness) the later calls fall through to a live Graph lookup.
	var toCache map[string]*client.ServicePrincipal
	if opts.Session != nil {
		toCache = make(map[string]*client.ServicePrincipal)
	}
	for _, sp := range resp.Value {
		if _, ok := orgIDs[sp.AppOwnerOrganizationId]; ok {
			if toCache != nil {
				toCache[sp.ID] = sp
			}
			applicationsOwned = append(applicationsOwned, sp)
		}
	}
	if len(toCache) > 0 {
		_ = session.SetManyJSON(ctx, opts.Session, toCache, sessions.WithPrefix(servicePrincipalPrefix))
	}

	resources := make([]*v2.Resource, len(applicationsOwned))

	for i, app := range applicationsOwned {
		value, err := enterpriseApplicationResource(ctx, app, parentResourceID)
		if err != nil {
			return nil, nil, err
		}

		resources[i] = value
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, nil, err
	}

	return resources, &rs.SyncOpResults{NextPageToken: pageToken}, nil
}

func (e *enterpriseApplicationsBuilder) Entitlements(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	var err error

	// https://learn.microsoft.com/en-us/graph/api/resources/approleassignment?view=graph-rest-1.0
	var rv []*v2.Entitlement

	{
		ownersEntId := enterpriseApplicationsEntitlementId{
			Type: ownersStr,
		}

		ownersEntIdString, err := ownersEntId.MarshalString()
		if err != nil {
			return nil, nil, err
		}

		ent := entitlement.NewPermissionEntitlement(
			resource,
			ownersEntIdString,
			entitlement.WithGrantableTo(userResourceType),
			entitlement.WithDisplayName(fmt.Sprintf("%s Application Owner", resource.DisplayName)),
			entitlement.WithDescription(fmt.Sprintf("Owner of %s Application", resource.DisplayName)),
		)
		ent.Slug = "owner"
		rv = append(rv, ent)
	}

	{
		// NOTE:
		// "00000000-0000-0000-0000-000000000000" is the principal ID for the default app role.

		// Most people are assigned directly to app roles but some people could be assigned
		// to the app directly
		// normally this happens by assigning someone access to an app while the app has roles
		// but it's possible the app then gets roles, meaning we have someone with the default assignment
		// and then someone with a specific role assignment
		defaultAppRoleAssignmentStringer := enterpriseApplicationsEntitlementId{
			Type:      appRoleStr,
			AppRoleId: defaultAppRoleAssignmentID,
		}

		defaultAppRoleAssignmentStringerString, err := defaultAppRoleAssignmentStringer.MarshalString()
		if err != nil {
			return nil, nil, err
		}

		ent := entitlement.NewAssignmentEntitlement(
			resource,
			defaultAppRoleAssignmentStringerString,
			entitlement.WithGrantableTo(userResourceType, groupResourceType),
			entitlement.WithDisplayName(fmt.Sprintf("%s Application Assignment", resource.DisplayName)),
			entitlement.WithDescription(fmt.Sprintf("Assigned to %s Application", resource.DisplayName)),
		)
		ent.Slug = "assigned"
		rv = append(rv, ent)
	}

	principalId := resource.Id.Resource
	servicePrincipal, err := e.spFromSession(ctx, opts, principalId)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-azure-infrastructure: failed to get service principal: %w", err)
	}

	for _, appRole := range servicePrincipal.AppRoles {
		if !slices.Contains(appRole.AllowedMemberTypes, "User") {
			continue
		}

		appRoleAssignmentId := enterpriseApplicationsEntitlementId{
			Type:      appRoleStr,
			AppRoleId: appRole.Id,
		}

		slug := appRole.Value
		if slug == "" {
			slug = appRole.DisplayName
		}

		appRoleAssignmentIdString, err := appRoleAssignmentId.MarshalString()
		if err != nil {
			return nil, nil, err
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

	return rv, nil, nil
}

func (e *enterpriseApplicationsBuilder) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	l := ctxzap.Extract(ctx)

	b := &pagination.Bag{}
	err := b.Unmarshal(opts.PageToken.Token)
	if err != nil {
		return nil, nil, err
	}

	// AzureId relarted to Azure resource
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
		principalResp, err := e.spFromSession(ctx, opts, principalId)
		if err != nil {
			return nil, nil, err
		}

		resp := principalResp.AppRolesAssignedTo
		grants, err := ConvertErr(resp, func(appRoleAssignment *client.AppRoleAssignment) (*v2.Grant, error) {
			var options []grant.GrantOption

			rid := &v2.ResourceId{Resource: appRoleAssignment.PrincipalId}
			switch appRoleAssignment.PrincipalType {
			case "User":
				rid.ResourceType = userResourceType.Id
			case "Group":
				rid.ResourceType = groupResourceType.Id

				options = append(options, grant.WithAnnotation(&v2.GrantExpandable{
					EntitlementIds: []string{
						fmt.Sprintf("group:%s:members", appRoleAssignment.PrincipalId),
					},
					Shallow:         true,
					ResourceTypeIds: []string{userResourceType.Id},
				}))
			case "ServicePrincipal":
				// TODO: service principals can be managed identities, enterprise applications, or maybe something else entirely.
				// We need to figure out the resource type instead of hard coding it to be a managed identity.
				rid.ResourceType = managedIdentitylResourceType.Id
				// rid.ResourceType = enterpriseApplicationResourceType.AzureId
			default:
				l.Error("baton-azure-infrastructure: unsupported PrincipalType type on app role assignment", zap.String("principal_type", appRoleAssignment.PrincipalType))
			}

			return grant.NewGrant(
				resource,
				fmt.Sprintf("assignment:%s",
					appRoleAssignment.AppRoleId,
				),
				rid,
				options...,
			), nil
		})
		if err != nil {
			return nil, nil, err
		}

		b.Pop()
		nextToken, err := b.Marshal()
		if err != nil {
			return nil, nil, err
		}

		return grants, &rs.SyncOpResults{NextPageToken: nextToken}, err
	case ownersStr:
		resp, err := e.client.ServicePrincipalOwners(ctx, principalId)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				ctxzap.Extract(ctx).Warn(
					"app role owner membership not found",
					zap.String("app_role_assignment_id", resource.Id.GetResource()),
					zap.String("url", ps.Token),
					zap.Error(err),
				)
				return nil, nil, nil
			}

			return nil, nil, err
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

			return grant.NewGrant(
				resource,
				"owners",
				rid,
			), nil
		})
		if err != nil {
			return nil, nil, err
		}

		pageToken, err := b.NextToken(resp.NextLink)
		if err != nil {
			return nil, nil, err
		}

		return grants, &rs.SyncOpResults{NextPageToken: pageToken}, nil
	default:
		return nil, nil, fmt.Errorf("unknown resource type: %s", ps.ResourceTypeID)
	}
}

func newEnterpriseApplicationsBuilder(c *Connector) *enterpriseApplicationsBuilder {
	return &enterpriseApplicationsBuilder{
		client:       c.client,
		conn:         c,
		skipAdGroups: c.SkipAdGroups,
	}
}

type enterpriseApplicationsEntitlementId struct {
	Type      string
	AppRoleId string
}

func (id *enterpriseApplicationsEntitlementId) MarshalString() (string, error) {
	switch id.Type {
	case appRoleStr:
		return strings.Join(
			[]string{
				assignmentStr,
				id.AppRoleId,
			},
			":"), nil
	case ownersStr:
		return strings.Join(
			[]string{
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
		return errors.New("baton-azure-infrastructure: invalid entitlement id")
	}
	id.Type = parts[2]
	if id.Type == assignmentStr {
		if len(parts) < 4 {
			return errors.New("baton-azure-infrastructure: invalid entitlement id: missing approle id")
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
	case ownersStr:
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
		return nil, fmt.Errorf("baton-microsoft-entra: only can provision app roles or owners entitlements to an enterprise application, got %s", eaEntId.Type)
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
		servicePrincipal, err := o.client.ServicePrincipal(ctx, resourceID)
		if err != nil {
			return nil, err
		}

		var roleAssignment *client.AppRoleAssignment
		for _, assignment := range servicePrincipal.AppRolesAssignedTo {
			if assignment.AppRoleId == eaEntId.AppRoleId {
				roleAssignment = assignment
			}
		}

		if roleAssignment == nil {
			return nil, fmt.Errorf("baton-azure-infrastructure: app role assignment not found for role id %s", grant.Principal.Id.Resource)
		}

		err = o.client.ServicePrincipalDeleteAppRoleAssignedTo(ctx, resourceID, roleAssignment.Id)
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
