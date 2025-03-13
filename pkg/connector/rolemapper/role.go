package rolemapper

import (
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
)

type RoleValue map[string]string

type AzureRoleActionMapper struct {
	roleToValue RoleValue
	// Computed section from roleToValue
	prefix string
}

func NewRoleActionMapper(prefix string, values []string) *AzureRoleActionMapper {
	roleToValue := make(map[string]string)
	for _, value := range values {
		roleToValue[prefix+value] = value
	}

	return &AzureRoleActionMapper{
		roleToValue: roleToValue,
		prefix:      prefix,
	}
}

func (r *AzureRoleActionMapper) MapRoleToAzureRoleAction(permissions []*armauthorization.Permission) ([]string, error) {
	actionsMap := map[string]string{}
	notActionsMap := map[string]string{}

	for _, permission := range permissions {
		for _, action := range parseToStringArray(permission.Actions) {
			roleAction := r.GetRoleAction(action)

			for _, ra := range roleAction {
				actionsMap[ra] = ra
			}
		}

		for _, action := range parseToStringArray(permission.NotActions) {
			roleAction := r.GetRoleAction(action)

			for _, ra := range roleAction {
				notActionsMap[ra] = ra
			}
		}
	}

	// Remove notActions from actions
	for k := range notActionsMap {
		delete(actionsMap, k)
	}

	var actions []string
	for k := range actionsMap {
		actions = append(actions, k)
	}

	return actions, nil
}

func (r *AzureRoleActionMapper) GetRoleAction(action string) []string {
	if action == "*" {
		return r.allActions()
	}

	if strings.HasPrefix(action, "*/") {
		actionV := strings.TrimPrefix(action, "*/")

		for _, v := range r.roleToValue {
			if v == actionV {
				return []string{actionV}
			}
		}

		return []string{}
	}

	if !strings.HasPrefix(action, r.prefix) {
		return []string{}
	}

	if strings.TrimPrefix(action, r.prefix) == "*" {
		return r.allActions()
	}

	if value, ok := r.roleToValue[action]; ok {
		return []string{value}
	}

	return nil
}

func (r *AzureRoleActionMapper) allActions() []string {
	var actions []string
	for _, v := range r.roleToValue {
		actions = append(actions, v)
	}

	return actions
}

func (r *AzureRoleActionMapper) Actions() RoleValue {
	actions := make(RoleValue)
	for k, v := range r.roleToValue {
		actions[k] = v
	}

	return actions
}

func parseToStringArray(param []*string) []string {
	var result []string
	for _, s := range param {
		if s != nil {
			result = append(result, *s)
		}
	}

	return result
}
