// constants.go
package connector

// Object types returned by Microsoft Graph.
const (
	odataTypeGroup            = "#microsoft.graph.group"
	odataTypeUser             = "#microsoft.graph.user"
	odataTypeServicePrincipal = "#microsoft.graph.servicePrincipal"
	odataTypeDevice           = "#microsoft.graph.device"
)

// Service-principal “servicePrincipalType” values.
const (
	spTypeApplication     = "Application"
	spTypeManagedIdentity = "ManagedIdentity"
	spTypeLegacy          = "Legacy"
	spTypeSocialIdp       = "SocialIdp"
)

// Baton entitlement.
const (
	typeOwners   = "owners"
	typeMembers  = "members"
	typeAssigned = "assigned"
)
