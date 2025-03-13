package connector

import (
	"context"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	resource "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type userBuilder struct {
	client          *client.AzureClient
	mailboxSettings bool
}

func (usr *userBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return userResourceType
}

// List returns all the users from the database as resource objects.
// Users include a UserTrait because they are the 'shape' of a standard user.
func (usr *userBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	resp, err := usr.client.Users(ctx, pToken.Token)
	if err != nil {
		return nil, "", nil, err
	}

	// If mailboxSettings is disabled, we can return the users without checking mailboxSettings.
	if !usr.mailboxSettings {
		users, err := ConvertErr(resp.Users, func(user *client.User) (*v2.Resource, error) {
			return userResource(ctx, user, parentResourceID)
		})
		if err != nil {
			return nil, "", nil, err
		}

		return users, resp.NextLink, nil, nil
	}

	var userResources []*v2.Resource

	// GET https://graph.microsoft.com/beta/users/{userId}/mailboxSettings
	for _, ur := range resp.Users {
		mailboxSettingsResp, err := usr.client.UserMailboxSetting(ctx, ur.ID)
		if err != nil {
			// TODO: previous version was just log.Warn, should we return an error here?
			return nil, "", nil, err
		}

		userPurpose := strings.ToLower(mailboxSettingsResp.UserPurpose)
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

	return userResources, resp.NextLink, nil, nil
}

// Entitlements always returns an empty slice for users.
func (usr *userBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (usr *userBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newUserBuilder(conn *Connector) *userBuilder {
	return &userBuilder{
		client:          conn.client,
		mailboxSettings: conn.MailboxSettings,
	}
}
