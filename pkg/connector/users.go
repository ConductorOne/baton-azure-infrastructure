package connector

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/client"
	"github.com/conductorone/baton-azure-infrastructure/pkg/internal/slices"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type userBuilder struct {
	c *Connector
}

func (u *userBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return userResourceType
}

// List returns all the users from the database as resource objects.
// Users include a UserTrait because they are the 'shape' of a standard user.
func (u *userBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	bag, err := parsePageToken(pToken.Token, &v2.ResourceId{ResourceType: userResourceType.Id})
	if err != nil {
		return nil, "", nil, err
	}
	reqURL := bag.PageToken()
	if reqURL == "" {
		v := url.Values{}
		v.Set("$select", strings.Join([]string{
			"id",
			"displayName",
			"mail",
			"userPrincipalName",
			"jobTitle",
			"manager",
			"accountEnabled",
			"employeeType",
			"employeeHireDate",
			"employeeId",
			"department",
		}, ","))
		v.Set("$expand", "manager($select=id,employeeId,mail,displayName)")
		v.Set("$top", "999")
		reqURL = u.c.buildURL("users", v)
	}

	resp := &client.UsersList{}
	err = u.c.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, resp)
	if err != nil {
		return nil, "", nil, err
	}

	pageToken, err := bag.NextToken(resp.NextLink)
	if err != nil {
		return nil, "", nil, err
	}

	// If mailboxSettings is disabled, we can return the users without checking mailboxSettings.
	if !u.c.MailboxSettings {
		users, err := slices.ConvertErr(resp.Users, func(u *client.User) (*v2.Resource, error) {
			return userResource(ctx, u, parentResourceID)
		})
		if err != nil {
			return nil, "", nil, err
		}

		return users, pageToken, nil, nil
	}

	userResources := make([]*v2.Resource, 0, len(resp.Users))
	// GET https://graph.microsoft.com/beta/users/{userId}/mailboxSettings
	for _, ur := range resp.Users {
		v := url.Values{}
		v.Set("$select", strings.Join([]string{
			"userPurpose",
		}, ","))
		reqURL = u.c.buildURL(path.Join("users", ur.ID, "mailboxSettings"), v)
		mailboxSettingsResp := &client.MailboxSettings{}
		userPurpose := "user"
		err = u.c.query(ctx, graphReadScopes, http.MethodGet, reqURL, nil, mailboxSettingsResp)
		if err == nil {
			userPurpose = strings.ToLower(mailboxSettingsResp.UserPurpose)
		} else {
			l.Warn(
				"baton-microsoft-entra: error fetching mailboxSettings",
				zap.Any("user", ur),
				zap.Error(err),
			)
		}

		userAccountType := resource.WithAccountType(v2.UserTrait_ACCOUNT_TYPE_HUMAN)
		switch userPurpose {
		case "room", "equipment", "shared":
			userAccountType = resource.WithAccountType(v2.UserTrait_ACCOUNT_TYPE_SERVICE)
		}

		userResource, err := userResource(ctx, ur, parentResourceID, userAccountType)
		if err != nil {
			return nil, "", nil, err
		}
		userResources = append(userResources, userResource)
	}

	return userResources, pageToken, nil, nil
}

// Entitlements always returns an empty slice for users.
func (o *userBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (u *userBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newUserBuilder(conn *Connector) *userBuilder {
	return &userBuilder{c: conn}
}
