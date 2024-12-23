package connector

import (
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	annotations "github.com/conductorone/baton-sdk/pkg/annotations"
)

var (
	// The user resource type is for all user objects from the database.
	userResourceType = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
	}

	groupResourceType = &v2.ResourceType{
		Id:          "group",
		DisplayName: "Group",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_GROUP},
	}

	enterpriseApplicationResourceType = &v2.ResourceType{
		Id:          "enterprise_application",
		DisplayName: "Enterprise Application",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_APP},
	}

	managedIdentitylResourceType = &v2.ResourceType{
		Id:          "managed_identity",
		DisplayName: "Managed Identity",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
		Annotations: annotations.New(&v2.SkipEntitlementsAndGrants{}),
	}

	subscriptionsResourceType = &v2.ResourceType{
		Id:          "subscriptions",
		DisplayName: "Subscriptions Application",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_APP},
	}
)
