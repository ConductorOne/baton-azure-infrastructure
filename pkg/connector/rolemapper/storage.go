package rolemapper

var StorageAccountPermissions = NewRoleActionMapper(
	"Microsoft.Storage/storageAccounts/",
	[]string{
		"read",
		"write",
		"delete",
	},
)

var ContainerPermissions = NewRoleActionMapper(
	"Microsoft.Storage/storageAccounts/blobServices/containers/",
	[]string{
		"read",
		"write",
		"delete",
	},
)
