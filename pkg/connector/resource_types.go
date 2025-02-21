package connector

import (
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	annotations "github.com/conductorone/baton-sdk/pkg/annotations"
)

var (
	// The user resource type is for all user objects from the database.
	userResourceType = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
	}

	groupResourceType = &v2.ResourceType{
		Id:          "group",
		DisplayName: "Group of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_GROUP},
	}

	enterpriseApplicationResourceType = &v2.ResourceType{
		Id:          "enterprise_application",
		DisplayName: "Enterprise Application of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_APP},
	}

	managedIdentitylResourceType = &v2.ResourceType{
		Id:          "managed_identity",
		DisplayName: "Managed Identity of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
		Annotations: annotations.New(&v2.SkipEntitlementsAndGrants{}),
	}

	subscriptionsResourceType = &v2.ResourceType{
		Id:          "subscription",
		DisplayName: "Subscription of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_APP},
	}

	tenantResourceType = &v2.ResourceType{
		Id:          "tenant",
		DisplayName: "Tenant of Azure Infrastructure",
	}

	resourceGroupResourceType = &v2.ResourceType{
		Id:          "resource_group",
		DisplayName: "Resource Group of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_GROUP},
	}

	roleAssignmentResourceGroupType = &v2.ResourceType{
		Id:          "resource_group_role_assignment",
		DisplayName: "Role Assignment Resource Group of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_GROUP},
	}

	roleResourceType = &v2.ResourceType{
		Id:          "role",
		DisplayName: "Role",
		Description: "Role of Azure Infrastructure",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_ROLE},
	}
)
