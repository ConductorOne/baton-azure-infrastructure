package connector

import (
	"context"
	"strings"

	"github.com/conductorone/baton-azure-infrastructure/pkg/connector/client"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
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
func (usr *userBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	resp, err := usr.client.Users(ctx, opts.PageToken.Token)
	if err != nil {
		return nil, nil, err
	}

	// If mailboxSettings is disabled, we can return the users without checking mailboxSettings.
	if !usr.mailboxSettings {
		users, err := ConvertErr(resp.Users, func(user *client.User) (*v2.Resource, error) {
			return userResource(ctx, user, parentResourceID)
		})
		if err != nil {
			return nil, nil, err
		}

		return users, &rs.SyncOpResults{NextPageToken: resp.NextLink}, nil
	}

	var userResources []*v2.Resource
	l := ctxzap.Extract(ctx)

	// GET https://graph.microsoft.com/beta/users/{userId}/mailboxSettings
	for _, ur := range resp.Users {
		mailboxSettingsResp, err := usr.client.UserMailboxSetting(ctx, ur.ID)
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				l.Debug("UserMailboxSetting: user not found", zap.String("user_id", ur.ID), zap.Error(err))
				continue
			}
			return nil, nil, err
		}

		userPurpose := strings.ToLower(mailboxSettingsResp.UserPurpose)
		userAccountType := rs.WithAccountType(v2.UserTrait_ACCOUNT_TYPE_HUMAN)
		switch userPurpose {
		case "room", "equipment", "shared":
			userAccountType = rs.WithAccountType(v2.UserTrait_ACCOUNT_TYPE_SERVICE)
		}

		userResource, err := userResource(ctx, ur, parentResourceID, userAccountType)
		if err != nil {
			return nil, nil, err
		}

		userResources = append(userResources, userResource)
	}

	return userResources, &rs.SyncOpResults{NextPageToken: resp.NextLink}, nil
}

// Entitlements always returns an empty slice for users.
func (usr *userBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (usr *userBuilder) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func newUserBuilder(conn *Connector) *userBuilder {
	return &userBuilder{
		client:          conn.client,
		mailboxSettings: conn.MailboxSettings,
	}
}
