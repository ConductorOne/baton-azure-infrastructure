package connector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	goslices "slices"

	"github.com/conductorone/baton-azure-infrastructure/pkg/internal/slices"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type enterpriseApplicationsBuilder struct {
	conn            *Connector
	cache           map[string]*servicePrincipal
	mu              sync.RWMutex
	organizationIDs []string
}

func (e *enterpriseApplicationsBuilder) cacheSet(id string, value *servicePrincipal) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache[id] = value
}

func (e *enterpriseApplicationsBuilder) cacheGet(id string) *servicePrincipal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cache[id]
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
	if reqURL == "" {
		urlValues := setEnterpriseApplicationsKeys()
		urlValues.Set("$expand", "appRoleAssignedTo")
		reqURL = e.conn.buildBetaURL("servicePrincipals", urlValues)
	}

	resp := &servicePrincipalsList{}
	err = e.conn.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, "", nil, err
	}

	applicationsOwned := []*servicePrincipal{}

	for _, sp := range resp.Value {
		if goslices.Contains(e.organizationIDs, sp.AppOwnerOrganizationId) {
			e.cacheSet(sp.ID, sp)
			applicationsOwned = append(applicationsOwned, sp)
		}
	}

	entApps, err := slices.ConvertErr(applicationsOwned, func(app *servicePrincipal) (*v2.Resource, error) {
		return enterpriseApplicationResource(ctx, app, parentResourceID)
	})
	if err != nil {
		return nil, "", nil, err
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	return entApps, pageToken, nil, nil
}

func (e *enterpriseApplicationsBuilder) Entitlements(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	ownersEntid := enterpriseApplicationsEntitlementId{
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
	ownersEntidString, err := ownersEntid.MarshalString()
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
			Id:          ownersEntidString,
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

	servicePrincipal := e.cacheGet(resource.Id.Resource)
	for _, appRole := range servicePrincipal.AppRoles {
		usersAllowed := false
		for _, memberType := range appRole.AllowedMemberTypes {
			if memberType == "User" {
				usersAllowed = true
				break
			}
		}

		if !usersAllowed {
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

		rv = append(rv, &v2.Entitlement{
			Id:          appRoleAssignmentIdString,
			Resource:    resource,
			DisplayName: fmt.Sprintf("%s Role Assignment", appRole.DisplayName),
			Description: fmt.Sprintf("Assigned to %s Application with %s Role", resource.DisplayName, appRole.Description),
			GrantableTo: []*v2.ResourceType{userResourceType, groupResourceType},
			Purpose:     v2.Entitlement_PURPOSE_VALUE_ASSIGNMENT,
			Slug:        slug,
		})
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
		resource.Id.Resource = strings.TrimPrefix(resource.Id.Resource, "applications/")
		ownersQuery := url.Values{}
		ownersQuery.Set("$select", strings.Join([]string{
			"id",
		}, ","))
		if e.conn.SkipAdGroups {
			ownersQuery.Set("$filter", "(onPremisesSyncEnabled ne true)")
			ownersQuery.Set("$count", "true") // Required to prevent MS Graph from returning a 400
		}

		ownersURL := e.conn.buildBetaURL(path.Join("servicePrincipals", resource.Id.Resource, ownersStr), ownersQuery)
		b.Push(pagination.PageState{
			ResourceTypeID: ownersStr,
			Token:          ownersURL,
		})

		appRoleAssignedToQuery := url.Values{}
		appRoleAssignedToURL := e.conn.buildBetaURL(path.Join("servicePrincipals", resource.Id.Resource, "appRoleAssignedTo"), appRoleAssignedToQuery)
		b.Push(pagination.PageState{
			ResourceTypeID: assignmentStr,
			Token:          appRoleAssignedToURL,
		})
	}

	ps := b.Current()
	switch ps.ResourceTypeID {
	case assignmentStr:
		resp := e.cacheGet(resource.Id.Resource).AppRolesAssignedTo
		grants, err := slices.ConvertErr(resp, func(ara *appRoleAssignment) (*v2.Grant, error) {
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
		resp := &membershipList{}
		err = e.conn.query(ctx, graphReadScopes, http.MethodGet, ps.Token, nil, resp)
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

		grants, err := slices.ConvertErr(resp.Members, func(gm *membership) (*v2.Grant, error) {
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
	return &enterpriseApplicationsBuilder{
		conn:            c,
		cache:           make(map[string]*servicePrincipal),
		organizationIDs: c.organizationIDs,
	}
}

type enterpriseApplicationsEntitlementId struct {
	Type      string
	Resource  string
	AppRoleId string
}

func (id *enterpriseApplicationsEntitlementId) MarshalString() (string, error) {
	switch id.Type {
	case "appRole":
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

	var reqURL string
	v := url.Values{}
	resourceID := entitlement.Resource.Id.Resource
	switch eaEntId.Type {
	case "owners":
		var reqBody *bytes.Reader
		// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-list-owners?view=graph-rest-1.0&tabs=http
		// POST /servicePrincipals/{id}/owners/$ref
		objRef := url.URL{
			Scheme: "https",
			Host:   "graph.microsoft.com",
			Path:   path.Join("v1.0", "directoryObjects", principal.Id.Resource),
		}
		reqURL = o.conn.buildURL(path.Join("servicePrincipals", resourceID, "owners", "$ref"), v)
		reqBody, err = (&assignment{
			ObjectRef: objRef.String(),
		}).MarshalToReader()
		if err != nil {
			return nil, err
		}
		err = o.conn.query(ctx, graphReadScopes, http.MethodPost, reqURL, reqBody, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to grant entitlement: %w", err)
		}

	case assignmentStr:
		var reqBody map[string]any
		// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-post-approleassignedto?view=graph-rest-1.0&tabs=http
		// POST /servicePrincipals/{id}/appRoleAssignedTo
		reqURL = o.conn.buildBetaURL(path.Join("servicePrincipals", resourceID, "appRoleAssignedTo"), v)
		reqBody = map[string]any{
			"appRoleId":   eaEntId.AppRoleId,
			"principalId": principal.Id.Resource,
			"resourceId":  resourceID,
		}
		err = o.conn.query(ctx, graphReadScopes, http.MethodPost, reqURL, reqBody, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to grant entitlement: %w", err)
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

	var reqURL string
	v := url.Values{}
	resourceID := grant.Entitlement.Resource.Id.Resource
	switch eaEntId.Type {
	case ownersStr:
		// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-delete-owners?view=graph-rest-1.0&tabs=http
		// DELETE /servicePrincipals/{id}/owners/{id}/$ref
		reqURL = o.conn.buildURL(path.Join("servicePrincipals", resourceID, "owners", grant.Principal.Id.Resource, "$ref"), v)
	case assignmentStr:
		// https://learn.microsoft.com/en-us/graph/api/serviceprincipal-delete-approleassignedto?view=graph-rest-1.0&tabs=http
		// DELETE /servicePrincipals/{id}/appRoleAssignedTo/{id}
		reqURL = o.conn.buildURL(path.Join("servicePrincipals", resourceID, "appRoleAssignedTo", grant.Id), v)
	default:
		l.Warn(
			"baton-microsoft-entra: only can revoke app roles or owners entitlements to an enterprise application",
			zap.String("entitlement_id", grant.Entitlement.Id),
		)
		return nil, errors.New("baton-microsoft-entra: only can revoke app roles or owners entitlements to an enterprise application")
	}

	err = o.conn.query(ctx, graphReadScopes, http.MethodDelete, reqURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to revoke grant: %w", err)
	}

	return nil, nil
}
