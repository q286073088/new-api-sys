package setting

import (
	"errors"
	"slices"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const InvoiceSettingKey = "InvoiceSetting"

type InvoiceSetting struct {
	Enabled  bool  `json:"enabled"`
	AllUsers bool  `json:"all_users"`
	UserIDs  []int `json:"user_ids"`
}

var invoiceSetting = struct {
	sync.RWMutex
	value InvoiceSetting
}{}

func GetInvoiceSetting() InvoiceSetting {
	invoiceSetting.RLock()
	defer invoiceSetting.RUnlock()
	value := invoiceSetting.value
	value.UserIDs = slices.Clone(value.UserIDs)
	return value
}

func (s InvoiceSetting) Allows(userID int) bool {
	return s.Enabled && userID > 0 && (s.AllUsers || slices.Contains(s.UserIDs, userID))
}

func ParseInvoiceSetting(value string) (InvoiceSetting, error) {
	var s *InvoiceSetting
	if err := common.UnmarshalJsonStr(value, &s); err != nil {
		return InvoiceSetting{}, err
	}
	if s == nil || len(s.UserIDs) > 10000 {
		return InvoiceSetting{}, errors.New("invalid invoice settings")
	}
	for _, id := range s.UserIDs {
		if id <= 0 {
			return InvoiceSetting{}, errors.New("invoice user IDs must be positive")
		}
	}
	return *s, nil
}

func UpdateInvoiceSetting(value string) error {
	s, err := ParseInvoiceSetting(value)
	if err != nil {
		return err
	}
	invoiceSetting.Lock()
	defer invoiceSetting.Unlock()
	invoiceSetting.value = s
	return nil
}
