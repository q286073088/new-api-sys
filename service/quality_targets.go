package service

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// QualityTokenGroups preserves exactly the token's existing group permissions.
func QualityTokenGroups(token *model.Token, userGroup string) ([]string, error) {
	if token.Group == "auto" {
		groups, err := token.GetAutoGroups()
		if err != nil {
			return nil, err
		}
		if len(groups) == 0 {
			return GetUserAutoGroup(userGroup), nil
		}
		return FilterUserTokenAutoGroups(userGroup, groups), nil
	}
	group := token.Group
	if group == "" {
		group = userGroup
	}
	if !IsUserSelectableGroup(userGroup, group) {
		return nil, nil
	}
	return []string{group}, nil
}

func ValidateQualityTarget(target model.QualityTarget) (*model.Token, error) {
	if target.TokenID <= 0 || target.OwnerID <= 0 || strings.TrimSpace(target.Model) == "" || len(target.Model) > 255 || len(target.Group) > 255 || (target.Endpoint != "chat" && target.Endpoint != "responses") {
		return nil, errors.New("Invalid model, token, group or endpoint")
	}
	token, err := model.GetTokenByIds(target.TokenID, target.OwnerID)
	if err != nil {
		return nil, errors.New("API key is unavailable")
	}
	user, err := model.GetUserById(target.OwnerID, false)
	if err != nil || user.Role < common.RoleAdminUser || user.Status != common.UserStatusEnabled {
		return nil, errors.New("API key owner is not an active administrator")
	}
	groups, err := QualityTokenGroups(token, user.Group)
	if err != nil || !slices.Contains(groups, target.Group) {
		return nil, errors.New("API key cannot access this group")
	}
	if token.ModelLimitsEnabled && !token.GetModelLimitsMap()[target.Model] {
		return nil, errors.New("API key cannot access this model")
	}
	if !slices.Contains(model.GetGroupEnabledModels(target.Group), target.Model) {
		return nil, errors.New("Model is unavailable in this group")
	}
	// Expiry, balance, IP restrictions and account/token state are enforced again
	// by TokenAuth and normal relay billing, using the actual local request IP.
	return token, nil
}
